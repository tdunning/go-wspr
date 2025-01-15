package wspr

import (
	"device/rp"
	"fmt"
	"github.com/chiefMarlin/tinygo-drivers/si5351"
	pio "github.com/tinygo-org/pio/rp2-pio"
	"machine"
	"runtime/interrupt"
	"runtime/volatile"
	"time"
	"unsafe"
	"wspr/src/machine_x"
)

/*
This code handles the measurement of the frequency of a signal against an
external reference. There are a few gotchas in trying to do this:

- the PWM on the rp2040 doesn't have any way to use an external reference for
frequency counting.

- the DMA doesn't support triggering on external pins directly

- the PWM counter is only 16-bits long. If we count a 50MHz signal for one
second, we will get (very) many overflows. Even worse, these overflows will be so often,
we won't be able to guess how many. This applies at much lower frequencies as well.

- at any point, reading the PWM counter with the CPU is subject to latency that
isn't well-defined making the frequency measurements subject to noise.

To deal with these issues we do the following here:

- PWM0 runs a counter at the full speed of the external signal

- PWM1 runs a counter at 1/8192 of the full speed using an internal divider. PWM0 and
PWM1 are triggered at the same time. This means that the top 3 bits of PWM0 will match
the bottom 3 bits of PWM1 subject to a tiny bit of jitter since we can't read the
two values at exactly the same time.

- PWM2 runs a clock that increments 1000 times per second from the system clock. This
will wrap around every 65.536 seconds, but we can compensate by noticing when the count
decreases between samples.

- DMA0 transfers control blocks to DMA1 to move the PWM0 counter, the PWM1 counter,
the PWM2 counter and the PWM0 counter again to the PIO. Each time DMA0 is triggered,
it moves the next control block (consisting only of the READ address) which causes the
next value to be moved.

- DMA1 moves data to the PIO output FIFO and then chains back to DMA0

- the PIO waits until the negative transition on the external PPS signal and moves four
measurements to the output FIFO. This puts back-pressure on DMA1 (and indirectly on DMA0)
so that we see one sample per second. That data can be read by software at any time during
the next second.

- the actual measurement to 29bit precision is reconstructed by comparing the PWM1 value to
the PWM0 value. The PWM1 value should be the high bits of the PWM0 value and there should
be 3 bits that overlap. The PWM0 value is sampled before and after PWM1 so that we can
distinguish the case when PWM1 increments before we read it but after we read PWM0.
The total time between the first and second reads of PWM0 should be only a few memory cycles.
*/

//go:generate pioasm -o go timer.pio     timer.go

type Sample struct {
	T                                  uint64 // monotonic sample time in µs since powerup
	Count                              uint64 // cycle count typically within 30ns of sample time
	Type                               int    // 0=direct, 1=DMA
	TH1, TL1, TH2, TL2, B1, A1, B2, A2 uint32 // raw data
}

func Setup() (*chan Sample, error) {
	time.Sleep(1000 * time.Millisecond)
	setupFrequencyCounters()
	setupClock()
	setupDMA()
	return setupInterrupt()
}

var PwmReaders struct {
	Sm         pio.StateMachine
	D0, D1, D2 DmaChannel
}

// `result` is the buffer that the DMA hardware
// uses to store the PWM counters.
var result struct {
	th1, tl1, th2, tl2, b1, a1, b2, a2 volatile.Register32
}

// controlBlock contains DMA read and write addresses
type controlBlock struct {
	from uint32
	to   uint32
}

// transfers contains a list of DMA controls that
// read the PWM and write to the result structure
var transfers []controlBlock

// WaitCounter is a counter used to measure how long we have
// to wait for the DMA transfers
var WaitCounter volatile.Register32

func setupDMA() {
	sm, _ := pio.PIO0.ClaimStateMachine()
	_, _, err := TimerInit(sm)
	if err != nil {
		fmt.Printf("Error adding PIO program: %s\n", err)
		machine.EnterBootloader()
	}
	PwmReaders.Sm = sm
	sm.ClearFIFOs()
	sm.SetEnabled(true)

	// the purpose of d0 is to move a data count to d1
	// when the PIO detects an edge
	d0, ok := ClaimChannel()
	if !ok {
		panic("Failed to get DMA channel")
	}
	PwmReaders.D0 = d0

	// d1 controls the gather operation that moves data from
	// the PWM counters to memory by moving control
	// blocks to d2
	d1, ok := ClaimChannel()
	if !ok {
		panic("Failed to get DMA channel")
	}
	PwmReaders.D1 = d1

	// d2 actually does the gather operation. Each step
	// chains back to d1 to get the next control block
	d2, ok := ClaimChannel()
	if !ok {
		panic("Failed to get DMA channel")
	}
	PwmReaders.D2 = d2

	// d0 reads from PIO and writes to the trigger register of d1
	c0 := DefaultDMAConfig(d0.ChannelIndex())
	c0.SetReadIncrement(false)
	c0.SetWriteIncrement(false)
	c0.SetRing(false, 0)
	c0.SetTransferDataSize(DmaTxSize32)
	c0.SetTREQ_SEL(DmaPIO_RxDREQ(sm))
	c0.SetEnable(true)
	d0.DmaRegister(DMA_AL1_CTRL).Set(c0.CTRL)
	d0.DmaRegister(DMA_TRANS_COUNT).Set(0xffff_ffff)
	d0.DmaRegister(DMA_READ_ADDR).Set(uint32(uintptr(unsafe.Pointer(sm.RxReg()))))

	//// avoid triggering
	//d0.HW().READ_ADDR.Set(uint32(uintptr(unsafe.Pointer(&count[0]))))
	//d0.HW().WRITE_ADDR.Set(d1.DmaRegisterAddress(DMA_AL1_TRANS_COUNT_TRIG))

	c1 := DefaultDMAConfig(d1.ChannelIndex())
	fmt.Printf("cc = %x, addr = %x\n", c1.CTRL, d1.DmaRegister(DMA_AL1_CTRL).Get())
	// chain to self means no chaining
	c1.SetReadIncrement(true)
	c1.SetWriteIncrement(true)
	// write address wraps around after moving read and write addresses
	c1.SetRing(true, 3)
	c1.SetTransferDataSize(DmaTxSize32)
	c1.SetIRQQuiet(true)
	c1.SetEnable(false)
	d1.DmaRegister(DMA_AL1_CTRL).Set(c1.CTRL)

	t := rp.TIMER
	transfers = []controlBlock{
		{
			uint32(uintptr(unsafe.Pointer(&t.TIMERAWH))),
			uint32(uintptr(unsafe.Pointer(&result.th1))),
		},
		{
			uint32(uintptr(unsafe.Pointer(&t.TIMERAWL))),
			uint32(uintptr(unsafe.Pointer(&result.tl1))),
		},
		{
			uint32(uintptr(unsafe.Pointer(&t.TIMERAWH))),
			uint32(uintptr(unsafe.Pointer(&result.th2))),
		},
		{
			uint32(uintptr(unsafe.Pointer(&t.TIMERAWL))),
			uint32(uintptr(unsafe.Pointer(&result.tl2))),
		},
		{
			uint32(uintptr(unsafe.Pointer(&machine.PWM1.CTR.Reg))),
			uint32(uintptr(unsafe.Pointer(&result.b1))),
		},
		{
			uint32(uintptr(unsafe.Pointer(&machine.PWM0.CTR.Reg))),
			uint32(uintptr(unsafe.Pointer(&result.a1))),
		},
		{
			uint32(uintptr(unsafe.Pointer(&machine.PWM1.CTR.Reg))),
			uint32(uintptr(unsafe.Pointer(&result.b2))),
		},
		{
			uint32(uintptr(unsafe.Pointer(&machine.PWM0.CTR.Reg))),
			uint32(uintptr(unsafe.Pointer(&result.a2))),
		},
		{
			0,
			0,
		},
	}

	// d1 writes to DMA_AL2_READ_ADDR and DMA_AL2_WRITE_ADDR_TRIG on d2
	d1.DmaRegister(DMA_WRITE_ADDR).Set(uint32(d2.DmaRegisterAddress(DMA_AL2_READ_ADDR)))
	//fmt.Printf("read = %x, write = %x\n",
	//	d1.DmaRegister(DMA_AL1_READ_ADDR).Get(), d1.DmaRegister(DMA_AL1_WRITE_ADDR).Get())

	c2 := DefaultDMAConfig(d1.ChannelIndex())

	// chain to the control channel without block interrupts
	c2.SetIRQQuiet(true)
	c2.SetChainTo(d1.ChannelIndex())

	c2.SetRing(false, 0)
	c2.SetReadIncrement(true)
	c2.SetWriteIncrement(true)
	// PWM registers are 32 bits even if counts are limited to 2^16
	c2.SetTransferDataSize(DmaTxSize32)
	c2.SetEnable(true)
	d2.DmaRegister(DMA_AL2_CTRL).Set(c2.CTRL)

	dmaInterruptEnable(d2.ChannelIndex(), false)
	d2.DmaRegister(DMA_AL2_TRANS_COUNT).Set(1)
	fmt.Printf("  %x vs %x vs %x\n", d2.DmaRegister(DMA_AL2_CTRL).Get(), d2.HW().CTRL_TRIG.Get(), c2.CTRL)
	fmt.Printf("  %x should be 1\n", d2.HW().TRANS_COUNT)

	// arm d1
	//dmaInterruptEnable(d1.ChannelIndex(), true)
	d1.DmaRegister(DMA_AL1_CTRL).SetBits(rp.DMA_CH0_CTRL_TRIG_EN_Msk)

	// this will need to be reset after each gather operation
	d1.DmaRegister(DMA_READ_ADDR).Set(uint32(uintptr(unsafe.Pointer(&transfers[0]))))

	// this sets connects the PIO to d1 via d0
	// the PIO will emit a count on every edge of the pulse-per-second signal
	// d0 will see this in the receive FIFO and move it to the transfer count trigger for d1
	d0.DmaRegister(DMA_AL2_WRITE_ADDR_TRIG).Set(uint32(d1.DmaRegisterAddress(DMA_AL1_TRANS_COUNT_TRIG)))

	// d1 now will launch whenever a value of 2 is written to DMA_AL1_TRANS_COUNT_TRIG
	// as with
	//PwmReaders.d1.DmaRegister(DMA_AL1_TRANS_COUNT_TRIG).Set(2)
	// or by d0 moving a value from the PIO
}

const (
	InterruptOK = iota
	InterruptRunning
	FirstSendFailed
	SecondSendFailed
	NoDMAData
	DmaBusy
)

func InterruptMessage(code uint32) string {
	switch code {
	case InterruptOK:
		return "Interrupt OK"
	case InterruptRunning:
		return "Interrupt Running"
	case FirstSendFailed:
		return "First Send Failed"
	case SecondSendFailed:
		return "Second Send Failed"
	case NoDMAData:
		return "No DMA Data"
	case DmaBusy:
		return "DMA Busy"
	default:
		return fmt.Sprintf("Unknown interrupt message %d", code)
	}
}

var ErrorFlag volatile.Register32
var InterruptCounter volatile.Register32

func setupInterrupt() (*chan Sample, error) {
	p := machine.Pin(10)
	p.Configure(machine.PinConfig{Mode: machine.PinInputPullup})

	samples := make(chan Sample, 2)
	err := p.SetInterrupt(machine.PinRising, func(pin machine.Pin) {
		//PwmReaders.D1.DmaRegister(DMA_AL1_TRANS_COUNT_TRIG).Set(2)
		// DMA should have been triggered by the time we arrive, but ...
		for i := 0; i < 1000; i++ {
			if result.b2.Get() != 100 {
				break
			}
		}
		s := collectSample(DmaSampler{})
		select {
		case samples <- s:
			// sample sent
		default:
			ErrorFlag.Set(SecondSendFailed)
			return
		}
		if s.B1 != 0xffffffff {
			ErrorFlag.Set(InterruptOK)
		} else {
			ErrorFlag.Set(NoDMAData)
		}

		// set up for DMA transfer
		PwmReaders.D1.DmaRegister(DMA_READ_ADDR).Set(uint32(uintptr(unsafe.Pointer(&transfers[0]))))
	})
	if err != nil {
		return nil, err
	}
	irq := interrupt.New(rp.IRQ_DMA_IRQ_0, func(i interrupt.Interrupt) {
		InterruptCounter.Set(InterruptCounter.Get() + 1)
	})
	irq.Enable()
	return &samples, nil
}

const (
	fastCycle = 50_000
	slowCycle = 50_000
)

type Sampler interface {
	Collect() Sample
}

// DirectSampler reads the PWM counters directly without using DMA. This can mean
// that there is 1-2µs between the first and last sample even if GC doesn't step
// in.
type DirectSampler struct{}

func (d DirectSampler) Collect() Sample {
	t := rp.TIMER
	th1, tl1, th2, tl2 := t.TIMERAWH.Get(), t.TIMERAWL.Get(), t.TIMERAWH.Get(), t.TIMERAWL.Get()
	b1, a1, b2, a2 := SlowCount(), CurrentCount(),
		SlowCount(), CurrentCount()
	return Sample{
		TH1: th1,
		TL1: tl1,
		TH2: th2,
		TL2: tl2,
		B1:  b1,
		A1:  a1,
		B2:  b2,
		A2:  a2,
	}
}

// DmaSampler reads data that has previously been put into a well-known place by
// a DMA driven process. It is a little bit wrong to attach the time to this
// sample when it is read rather than when it was actually collected, but it is
// hard to get the current time via DMA. Note how the memory buffer is tainted to
// make it clear whether the next sample has real data.
type DmaSampler struct{}

func (d DmaSampler) Collect() Sample {
	r := Sample{
		TH1: result.th1.Get(),
		TL1: result.tl1.Get(),
		TH2: result.th2.Get(),
		TL2: result.tl2.Get(),
		B1:  result.b1.Get(),
		A1:  result.a1.Get(),
		B2:  result.b2.Get(),
		A2:  result.a2.Get(),
	}
	// put implausible value back in.
	result.b1.Set(0)
	result.b2.Set(100)
	return r
}

// collectSample retrieves a sample which consists of five consecutive samples of
// the PWM counters. These samples are from the slow (B) and fast (A) counters
// which we cannot sample at exactly the same time. This means that if we sample
// A, then B, B might increment after A is read but before B is read. The same
// happens if we read B, then A. To avoid these problems, we sample the counters
// in the order B1, A1, B2, A2, B3. Since we know (assume, really) that B can
// only increment at most once during our entire sampling process we can compare
// B1 vs B2 and B2 vs B3 (one of which must be true) and return either (B2, A1)
// or (B2, A2) as our result.
func collectSample(s Sampler) Sample {
	r := s.Collect()
	r.Count = ReduceObservation(fastCycle, r.B1, r.A1, r.B2, r.A2)
	r.T = ReduceObservation(1<<32, r.TH1, r.TL1, r.TH2, r.TL2)
	return r
}

func setupFrequencyCounters() {
	// set up to count with the fast and slow counters
	machine_x.SetEN_CH(machine_x.PWM_CH0|machine_x.PWM_CH1, 0)
	pwm0 := machine_x.PWM0
	machine.Pin(0).Configure(machine.PinConfig{Mode: machine.PinPWM})
	machine.Pin(1).Configure(machine.PinConfig{Mode: machine.PinPWM})
	pwm0.SetDivMode(rp.PWM_CH0_CSR_DIVMODE_RISE)
	pwm0.SetClockDiv(1, 0x0)

	pwm0.SetTop(fastCycle)
	pwm0.Set(0, 500)

	pwm1 := machine_x.PWM1
	machine.Pin(2).Configure(machine.PinConfig{Mode: machine.PinPWM})
	machine.Pin(3).Configure(machine.PinConfig{Mode: machine.PinPWM})
	pwm1.SetDivMode(rp.PWM_CH0_CSR_DIVMODE_RISE)
	pwm1.SetClockDiv(1, 0)
	pwm1.SetTop(slowCycle)
	pwm1.Set(0, 500)

	// enable both counters simultaneously
	machine_x.SetEN_CH(machine_x.PWM_CH0|machine_x.PWM_CH1, 1)
}

func setupClock() {
	// Configure I2C bus
	err := machine.I2C0.Configure(machine.I2CConfig{})
	if err != nil {
		panic("Failed to configure I2C0")
	}

	// Create driver instance
	clockgen := si5351.New(machine.I2C0)

	// Verify device wired properly
	connected, err := clockgen.Connected()
	if err != nil {
		panic("Unable to read device status")
	}
	if !connected {
		panic("Unable to connect to SI5351 device")
	}

	err = clockgen.Configure()
	if err != nil {
		panic("Unable to configure device")
	}

	// Now configure the PLLs for 750MHz = 24 * 25MHz
	pllMul := 30
	err = clockgen.ConfigurePLL(si5351.PLL_A, uint8(pllMul), 0, 1)
	if err != nil {
		panic("Unable to configure PLL")
	}
	fmt.Printf("PLL A frequency: %.1f", float64(pllMul)*25.0)

	// Now configure the clock output for 28.85MHz = 750MHz / 26
	div := uint32(26)
	err = clockgen.ConfigureMultisynth(0, si5351.PLL_A, div, 0, 1)
	if err != nil {
		panic(fmt.Errorf("unable to configure output %v", err))
	}
	fmt.Printf("Clock 0: %.3f kHz\n", 25e6*float64(pllMul)/float64(div)/1e3)

	err = clockgen.EnableOutputs()
	if err != nil {
		panic("Unable to enable outputs")
	}
}

func CurrentCount() uint32 {
	pwm0 := machine_x.PWM0
	return pwm0.Counter()
}

func SlowCount() uint32 {
	pwm1 := machine_x.PWM1
	return pwm1.Counter()
}
