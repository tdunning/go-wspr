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

package main

import (
	"fmt"
	"machine"
	"time"
	"wspr/src/pico"
)

func main() {
	samples, err := pico.Setup()
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

	t0 := pico.MicroTime()

	fmt.Printf("setup complete %s\n", pico.InterruptMessage(pico.ErrorFlag.Get()))
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
			if pico.ErrorFlag.Get() != 0 {
				fmt.Printf("ERROR = %s\n", pico.InterruptMessage(pico.ErrorFlag.Get()))
			}

			dt := float64(s.T-t0) * 1e-6
			fmt.Printf("fifo = %d\n", pico.PwmReaders.Sm.RxFIFOLevel())
			fmt.Printf("Δk = %d, Δt = %.7f, t = %.7f, Δa = %d, f = %.6f\n",
				s.Count-k0+offset, dt, float64(pico.MicroTime()-s.T)*1e-6, s.A2-s.A1, float64(s.Count-k0+offset)/dt)
			if s.B1 != s.B2 {
				fmt.Printf("   b1,a1,b2,a2,b3 = %d, %d, %d, %d\n", s.B1, s.A1, s.B2, s.A2)
			}
			fmt.Printf("   wait count = %d\n", pico.WaitCounter.Get())
			k0 = s.Count
			t0 = s.T
			missedSamples = 0
		}
	}
	machine.EnterBootloader()
}
