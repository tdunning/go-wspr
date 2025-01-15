//go:build rp2040

/*
 * Copyright 2025 Ted Dunning
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package wspr

import (
	"device/rp"
	"errors"
	pio "github.com/tinygo-org/pio/rp2-pio"
	"runtime"
	"runtime/volatile"
	"time"
	"unsafe"
)

var _DMA = &dmaArbiter{}

type dmaArbiter struct {
	claimedChannels uint16
}

func ClaimChannel() (channel DmaChannel, ok bool) {
	return _DMA.claimChannel()
}

// ClaimChannel returns a DMA channel that can be used for DMA transfers.
func (arb *dmaArbiter) claimChannel() (channel DmaChannel, ok bool) {
	for i := uint8(0); i < 12; i++ {
		ch := arb.Channel(i)
		if ch.TryClaim() {
			return ch, true
		}
	}
	return DmaChannel{}, false
}

func (arb *dmaArbiter) Channel(channel uint8) DmaChannel {
	if channel > 11 {
		panic("invalid DMA channel")
	}
	// DMA channels usable on the RP2040. 12 in total.
	var dmaChannels = (*[12]dmaChannelHW)(unsafe.Pointer(rp.DMA))
	return DmaChannel{
		hw:  &dmaChannels[channel],
		arb: arb,
		idx: channel,
	}
}

type DmaChannel struct {
	hw  *dmaChannelHW
	arb *dmaArbiter
	dl  deadliner
	idx uint8
}

// TryClaim claims the DMA channel for use by a peripheral and returns if it succeeded in claiming the channel.
func (ch DmaChannel) TryClaim() bool {
	ch.mustValid()
	if ch.IsClaimed() {
		return false
	}
	ch.arb.claimedChannels |= 1 << ch.idx
	return true
}

// Unclaim releases the DMA channel so it can be used by other peripherals.
// It does not check if the channel is currently claimed; it force-unclaims the channel.
func (ch DmaChannel) Unclaim() {
	ch.mustValid()
	ch.arb.claimedChannels &^= 1 << ch.idx
}

// IsClaimed returns true if the DMA channel is currently claimed through software.
func (ch DmaChannel) IsClaimed() bool {
	ch.mustValid()
	return ch.arb.claimedChannels&(1<<ch.idx) != 0
}

// IsValid returns true if the DMA channel was created successfully.
func (ch DmaChannel) IsValid() bool {
	return ch.hw != nil && ch.arb == _DMA
}

// ChannelIndex returns the channel number of the DMA channel. In range 0..11.
func (ch DmaChannel) ChannelIndex() uint8 { return ch.idx }

// HW returns the hardware registers for this DMA channel.
func (ch DmaChannel) HW() *dmaChannelHW { return ch.hw }

func (ch DmaChannel) Init(cfg dmaChannelConfig) {
	ch.mustValid()
	ch.HW().CTRL_TRIG.Set(cfg.CTRL)
}

// CurrentConfig copies the actual configuration of the DMA channel.
func (ch DmaChannel) CurrentConfig() dmaChannelConfig {
	ch.mustValid()
	return dmaChannelConfig{CTRL: ch.HW().CTRL_TRIG.Get()}
}

func (ch DmaChannel) mustValid() {
	if !ch.IsValid() {
		panic("use of uninitialized DMA channel")
	}
}

// Single DMA channel. See rp.DMA_Type.
//
//goland:noinspection GoSnakeCaseUsage
type dmaChannelHW struct {
	READ_ADDR   volatile.Register32
	WRITE_ADDR  volatile.Register32
	TRANS_COUNT volatile.Register32
	CTRL_TRIG   volatile.Register32
	_           [12]volatile.Register32 // aliases
}

type dmaRegisterOffset uint32

// register offsets for DMA channels
//
//goland:noinspection GoSnakeCaseUsage
const (
	DMA_READ_ADDR = dmaRegisterOffset(4 * iota)
	DMA_WRITE_ADDR
	DMA_TRANS_COUNT
	DMA_CTRL_TRIG
	DMA_AL1_CTRL
	DMA_AL1_READ_ADDR
	DMA_AL1_WRITE_ADDR
	DMA_AL1_TRANS_COUNT_TRIG
	DMA_AL2_CTRL
	DMA_AL2_TRANS_COUNT
	DMA_AL2_READ_ADDR
	DMA_AL2_WRITE_ADDR_TRIG
	DMA_AL3_CTRL
	DMA_AL3_WRITE_ADDR
	DMA_AL3_TRANS_COUNT
	DMA_AL3_READ_ADDR_TRIG
	DMA_END_MARKER = uint32(DMA_AL3_READ_ADDR_TRIG) + 4
)

func (ch DmaChannel) DmaRegisterAddress(register dmaRegisterOffset) uintptr {
	base := uintptr(unsafe.Pointer(rp.DMA))
	return (base + uintptr(ch.ChannelIndex())*uintptr(DMA_END_MARKER) + uintptr(register))
}

func (ch DmaChannel) DmaRegister(register dmaRegisterOffset) *volatile.Register32 {
	//goland:noinspection GoVetUnsafePointer
	return (*volatile.Register32)(unsafe.Pointer(ch.DmaRegisterAddress(register)))
}

// dmaPIO_TREQ returns the Tx DREQ signal for a PIO state machine.
func DmaPIO_TxDREQ(sm pio.StateMachine) uint32 {
	return _DREQ_PIO0_TX0 + uint32(sm.PIO().BlockIndex())*8 + uint32(sm.StateMachineIndex())
}

// dmaPIO_TREQ returns the Rx DREQ signal for a PIO state machine.
func DmaPIO_RxDREQ(sm pio.StateMachine) uint32 {
	return DmaPIO_TxDREQ(sm) + 4
}

func dmaInterruptEnable(channel uint8, enable bool) {
	if enable {
		rp.DMA.INTE0.SetBits(1 << channel)
	} else {
		rp.DMA.INTE0.ClearBits(1 << channel)
	}
}

// 2.5.3.1. System DREQ Table. Note: Another caveat is that multiple channels should not be connected to the same DREQ.
//
//goland:noinspection GoSnakeCaseUsage
const (
	_DREQ_PIO0_TX0   = 0x0
	_DREQ_PIO0_TX1   = 0x1
	_DREQ_PIO0_TX2   = 0x2
	_DREQ_PIO0_TX3   = 0x3
	_DREQ_PIO0_RX0   = 0x4
	_DREQ_PIO0_RX1   = 0x5
	_DREQ_PIO0_RX2   = 0x6
	_DREQ_PIO0_RX3   = 0x7
	_DREQ_PIO1_TX0   = 0x8
	_DREQ_PIO1_TX1   = 0x9
	_DREQ_PIO1_TX2   = 0xa
	_DREQ_PIO1_TX3   = 0xb
	_DREQ_PIO1_RX0   = 0xc
	_DREQ_PIO1_RX1   = 0xd
	_DREQ_PIO1_RX2   = 0xe
	_DREQ_PIO1_RX3   = 0xf
	_DREQ_SPI0_TX    = 0x10
	_DREQ_SPI0_RX    = 0x11
	_DREQ_SPI1_TX    = 0x12
	_DREQ_SPI1_RX    = 0x13
	_DREQ_UART0_TX   = 0x14
	_DREQ_UART0_RX   = 0x15
	_DREQ_UART1_TX   = 0x16
	_DREQ_UART1_RX   = 0x17
	_DREQ_PWM_WRAP0  = 0x18
	_DREQ_PWM_WRAP1  = 0x19
	_DREQ_PWM_WRAP2  = 0x1a
	_DREQ_PWM_WRAP3  = 0x1b
	_DREQ_PWM_WRAP4  = 0x1c
	_DREQ_PWM_WRAP5  = 0x1d
	_DREQ_PWM_WRAP6  = 0x1e
	_DREQ_PWM_WRAP7  = 0x1f
	_DREQ_I2C0_TX    = 0x20
	_DREQ_I2C0_RX    = 0x21
	_DREQ_I2C1_TX    = 0x22
	_DREQ_I2C1_RX    = 0x23
	_DREQ_ADC        = 0x24
	_DREQ_XIP_STREAM = 0x25
	_DREQ_XIP_SSITX  = 0x26
	_DREQ_XIP_SSIRX  = 0x27
)

// Push32 writes each element of src slice into the memory location at dst.
func (ch DmaChannel) Push32(dst *uint32, src []uint32, dreq uint32) error {
	return dmaPush(ch, dst, src, dreq)
}

// Push16 writes each element of src slice into the memory location at dst.
func (ch DmaChannel) Push16(dst *uint16, src []uint16, dreq uint32) error {
	return dmaPush(ch, dst, src, dreq)
}

// Push8 writes each element of src slice into the memory location at dst.
func (ch DmaChannel) Push8(dst *byte, src []byte, dreq uint32) error {
	return dmaPush(ch, dst, src, dreq)
}

// Push32 writes each element of src slice into the memory location at dst.
func dmaPush[T uint8 | uint16 | uint32](ch DmaChannel, dst *T, src []T, dreq uint32) error {
	// If currently busy we wait until safe to edit hardware registers.
	deadline := ch.dl.newDeadline()
	for ch.Busy() {
		if deadline.expired() {
			return errContentionTimeout
		}
		gosched()
	}

	hw := ch.HW()
	hw.CTRL_TRIG.ClearBits(rp.DMA_CH0_CTRL_TRIG_EN_Msk)
	srcPtr := uint32(uintptr(unsafe.Pointer(&src[0])))
	dstPtr := uint32(uintptr(unsafe.Pointer(dst)))
	hw.READ_ADDR.Set(srcPtr)
	hw.WRITE_ADDR.Set(dstPtr)
	hw.TRANS_COUNT.Set(uint32(len(src)))

	// memfence

	cc := ch.CurrentConfig()
	cc.SetTREQ_SEL(dreq)
	cc.SetTransferDataSize(dmaSize[T]())
	cc.SetChainTo(ch.idx)
	cc.SetReadIncrement(true)
	cc.SetWriteIncrement(false)
	cc.SetEnable(true)

	// We begin our DMA transfer here!
	hw.CTRL_TRIG.Set(cc.CTRL)

	deadline = ch.dl.newDeadline()
	for ch.Busy() {
		if deadline.expired() {
			ch.abort()
			return errTimeout
		}
		gosched()
	}
	hw.CTRL_TRIG.ClearBits(rp.DMA_CH0_CTRL_TRIG_EN_Msk)
	return nil
}

// Pull32 reads the memory location at src into dst slice, incrementing dst pointer but not src.
func (ch DmaChannel) Pull32(dst []uint32, src *uint32, dreq uint32) error {
	return dmaPull(ch, dst, src, dreq)
}

// Pull16 reads the memory location at src into dst slice, incrementing dst pointer but not src.
func (ch DmaChannel) Pull16(dst []uint16, src *uint16, dreq uint32) error {
	return dmaPull(ch, dst, src, dreq)
}

// Pull8 reads the memory location at src into dst slice, incrementing dst pointer but not src.
func (ch DmaChannel) Pull8(dst []byte, src *byte, dreq uint32) error {
	return dmaPull(ch, dst, src, dreq)
}

// Pull32 reads the memory location at src into dst slice, incrementing dst pointer but not src.
func dmaPull[T uint8 | uint16 | uint32](ch DmaChannel, dst []T, src *T, dreq uint32) error {
	// If currently busy we wait until safe to edit hardware registers.
	deadline := ch.dl.newDeadline()
	for ch.Busy() {
		if deadline.expired() {
			return errContentionTimeout
		}
		gosched()
	}

	hw := ch.HW()
	hw.CTRL_TRIG.ClearBits(rp.DMA_CH0_CTRL_TRIG_EN_Msk)
	srcPtr := uint32(uintptr(unsafe.Pointer(src)))
	dstPtr := uint32(uintptr(unsafe.Pointer(&dst[0])))
	hw.READ_ADDR.Set(srcPtr)
	hw.WRITE_ADDR.Set(dstPtr)
	hw.TRANS_COUNT.Set(uint32(len(dst)))

	// memfence

	cc := ch.CurrentConfig()
	cc.SetTREQ_SEL(dreq)
	cc.SetTransferDataSize(dmaSize[T]())
	cc.SetChainTo(ch.idx)
	cc.SetReadIncrement(false)
	cc.SetWriteIncrement(true)
	cc.SetEnable(true)

	// We begin our DMA transfer here!
	hw.CTRL_TRIG.Set(cc.CTRL)

	deadline = ch.dl.newDeadline()
	for ch.Busy() {
		if deadline.expired() {
			ch.abort()
			return errTimeout
		}
		gosched()
	}
	return nil
}

func dmaSize[T uint8 | uint16 | uint32]() DmaTxSize {
	var a T
	switch unsafe.Sizeof(a) {
	case 1:
		return DmaTxSize8
	case 2:
		return DmaTxSize16
	case 4:
		return DmaTxSize32
	default:
		panic("invalid DMA transfer size")
	}
}

// abort aborts the current transfer sequence on the channel and blocks until
// all in-flight transfers have been flushed through the address and data FIFOs.
// After this, it is safe to restart the channel.
func (ch DmaChannel) abort() {
	// Each bit corresponds to a channel. Writing a 1 aborts whatever transfer
	// sequence is in progress on that channel. The bit will remain high until
	// any in-flight transfers have been flushed through the address and data FIFOs.
	// After writing, this register must be polled until it returns all-zero.
	// Until this point, it is unsafe to restart the channel.
	chMask := uint32(1 << ch.idx)
	rp.DMA.CHAN_ABORT.Set(chMask)

	deadline := ch.dl.newDeadline()
	for rp.DMA.CHAN_ABORT.Get()&chMask != 0 {
		if deadline.expired() {
			println("DMA abort timeout")
			break
		}
		gosched()
	}
}

func (ch DmaChannel) Busy() bool {
	hw := ch.HW()
	return hw.CTRL_TRIG.Get()&rp.DMA_CH0_CTRL_TRIG_BUSY != 0
}

type DmaTxSize uint32

const (
	DmaTxSize8 DmaTxSize = iota
	DmaTxSize16
	DmaTxSize32
)

type dmaChannelConfig struct {
	CTRL uint32
}

func DefaultDMAConfig(channel uint8) (cc dmaChannelConfig) {
	cc.SetRing(false, 0)
	cc.SetBSwap(false)
	cc.SetIRQQuiet(false)
	cc.SetWriteIncrement(false)
	cc.SetSniffEnable(false)
	cc.SetHighPriority(false)

	cc.SetChainTo(channel)
	cc.SetTREQ_SEL(rp.DMA_CH0_CTRL_TRIG_TREQ_SEL_PERMANENT)
	cc.SetReadIncrement(true)
	cc.SetTransferDataSize(DmaTxSize32)
	// cc.setEnable(true)
	return cc
}

// Select a Transfer Request signal. The channel uses the transfer request signal
// to pace its data transfer rate. Sources for TREQ signals are internal (TIMERS)
// or external (DREQ, a Data Request from the system). 0x0 to 0x3a -> select DREQ n as TREQ
func (cc *dmaChannelConfig) SetTREQ_SEL(dreq uint32) {
	cc.CTRL = (cc.CTRL & ^uint32(rp.DMA_CH0_CTRL_TRIG_TREQ_SEL_Msk)) | (uint32(dreq) << rp.DMA_CH0_CTRL_TRIG_TREQ_SEL_Pos)
}

func (cc *dmaChannelConfig) SetChainTo(chainTo uint8) {
	cc.CTRL = (cc.CTRL & ^uint32(rp.DMA_CH0_CTRL_TRIG_CHAIN_TO_Msk)) | (uint32(chainTo) << rp.DMA_CH0_CTRL_TRIG_CHAIN_TO_Pos)
}

func (cc *dmaChannelConfig) SetTransferDataSize(size DmaTxSize) {
	cc.CTRL = (cc.CTRL & ^uint32(rp.DMA_CH0_CTRL_TRIG_DATA_SIZE_Msk)) | (uint32(size) << rp.DMA_CH0_CTRL_TRIG_DATA_SIZE_Pos)
}

func (cc *dmaChannelConfig) SetRing(write bool, sizeBits uint32) {
	/*
		static inline void channel_config_set_ring(dma_channel_config *c, bool write, uint size_bits) {
		    assert(size_bits < 32);
		    c->ctrl = (c->ctrl & ~(DMA_CH0_CTRL_TRIG_RING_SIZE_BITS | DMA_CH0_CTRL_TRIG_RING_SEL_BITS)) |
		              (size_bits << DMA_CH0_CTRL_TRIG_RING_SIZE_LSB) |
		              (write ? DMA_CH0_CTRL_TRIG_RING_SEL_BITS : 0);
		}
	*/
	cc.CTRL = (cc.CTRL & ^uint32(rp.DMA_CH0_CTRL_TRIG_RING_SIZE_Msk)) |
		(sizeBits << rp.DMA_CH0_CTRL_TRIG_RING_SIZE_Pos)
	setBitPos(&cc.CTRL, rp.DMA_CH0_CTRL_TRIG_RING_SEL_Pos, write)
}

func (cc *dmaChannelConfig) SetReadIncrement(incr bool) {
	setBitPos(&cc.CTRL, rp.DMA_CH0_CTRL_TRIG_INCR_READ_Pos, incr)
}

func (cc *dmaChannelConfig) SetWriteIncrement(incr bool) {
	setBitPos(&cc.CTRL, rp.DMA_CH0_CTRL_TRIG_INCR_WRITE_Pos, incr)
}

func (cc *dmaChannelConfig) SetBSwap(bswap bool) {
	setBitPos(&cc.CTRL, rp.DMA_CH0_CTRL_TRIG_BSWAP_Pos, bswap)
}

func (cc *dmaChannelConfig) SetIRQQuiet(irqQuiet bool) {
	setBitPos(&cc.CTRL, rp.DMA_CH0_CTRL_TRIG_IRQ_QUIET_Pos, irqQuiet)
}

func (cc *dmaChannelConfig) SetHighPriority(highPriority bool) {
	setBitPos(&cc.CTRL, rp.DMA_CH0_CTRL_TRIG_HIGH_PRIORITY_Pos, highPriority)
}

func (cc *dmaChannelConfig) SetEnable(enable bool) {
	setBitPos(&cc.CTRL, rp.DMA_CH0_CTRL_TRIG_EN_Pos, enable)
}

func (cc *dmaChannelConfig) SetSniffEnable(sniffEnable bool) {
	setBitPos(&cc.CTRL, rp.DMA_CH0_CTRL_TRIG_SNIFF_EN_Pos, sniffEnable)
}

func setBitPos(cc *uint32, pos uint32, bit bool) {
	if bit {
		*cc = *cc | (1 << pos)
	} else {
		*cc = *cc & ^(1 << pos) // unset bit.
	}
}

func ptrAs[T ~uint32](ptr *T) uint32 {
	return uint32(uintptr(unsafe.Pointer(ptr)))
}

var (
	errTimeout           = errors.New("piolib:timeout")
	errContentionTimeout = errors.New("piolib:contention timeout")
	errBusy              = errors.New("piolib:busy")

	errDMAUnavail = errors.New("piolib:DMA channel unavailable")
)

func gosched() {
	runtime.Gosched()
}

type deadline struct {
	t time.Time
}

func (dl deadline) expired() bool {
	if dl.t.IsZero() {
		return false
	}
	return time.Since(dl.t) > 0
}

type deadliner struct {
	// timeout is a bitshift value for the timeout.
	timeout uint8
}

func (ch deadliner) newDeadline() deadline {
	var t time.Time
	if ch.timeout != 0 {
		calc := time.Duration(1 << ch.timeout)
		t = time.Now().Add(calc)
	}
	return deadline{t: t}
}

func (ch *deadliner) setTimeout(timeout time.Duration) {
	if timeout <= 0 {
		ch.timeout = 0
		return // No timeout.
	}
	for i := uint8(0); i < 64; i++ {
		calc := time.Duration(1 << i)
		if calc > timeout {
			ch.timeout = i
			return
		}
	}
}
