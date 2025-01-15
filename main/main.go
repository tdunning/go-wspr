package main

import (
	"fmt"
	"machine"
	"time"
	"wspr/src/wspr"
)

func main() {
	samples, err := wspr.Setup()
	if err != nil {
		panic("failed setup: " + err.Error())
	}
	timeout := time.NewTicker(2 * time.Second)
	missedSamples := 0
	k0 := uint64(0)
	bcast := func(x bool) int {
		if x {
			return 1
		} else {
			return 0
		}
	}
	p := machine.Pin(10)
	p.Configure(machine.PinConfig{Mode: machine.PinInputPullup})
	q := machine.Pin(11)
	q.Configure(machine.PinConfig{
		Mode: machine.PinOutput,
	})

	t0 := wspr.MicroTime()

	fmt.Printf("setup complete %s\n", wspr.InterruptMessage(wspr.ErrorFlag.Get()))
	for i := 0; i < 100; i++ {
		select {
		case <-timeout.C:
			if missedSamples > 1 {
				fmt.Printf("timeout %d, pin=%d\n", missedSamples, bcast(p.Get()))
			}
			missedSamples++
		case s := <-*samples:
			offset := uint64(0)
			if s.Count < k0 {
				offset = 50_000 * 50_000
			}
			if wspr.ErrorFlag.Get() != 0 {
				fmt.Printf("ERROR = %s\n", wspr.InterruptMessage(wspr.ErrorFlag.Get()))
			}

			dt := float64(s.T-t0) * 1e-6
			fmt.Printf("fifo = %d\n", wspr.PwmReaders.Sm.RxFIFOLevel())
			fmt.Printf("Δk = %d, Δt = %.7f, t = %.7f, Δa = %d, f = %.6f\n",
				s.Count-k0+offset, dt, float64(wspr.MicroTime()-s.T)*1e-6, s.A2-s.A1, float64(s.Count-k0+offset)/dt)
			if s.B1 != s.B2 {
				fmt.Printf("   b1,a1,b2,a2,b3 = %d, %d, %d, %d\n", s.B1, s.A1, s.B2, s.A2)
			}
			fmt.Printf("   wait count = %d\n", wspr.WaitCounter.Get())
			k0 = s.Count
			t0 = s.T
			missedSamples = 0
		}
	}
	machine.EnterBootloader()
}
