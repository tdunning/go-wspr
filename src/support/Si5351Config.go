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

package support

import (
	"errors"
	"fmt"
	"math"
)

type Si5351Config struct {
	f0, pll, f                float64 // clock, pll and output frequencies
	a0, b0, c0, a1, b1, c1, r uint32  // chip parameters
	eps                       float64 // error in output frequency (Hz)
}

/*
Si5351Config computes configuration parameters for the PLL and multi-synth
fractional dividers in a Si5351 clock generator.

The parameter `f0` is the internal clock frequency (in Hz) for the generator (typically
25 or 27MHz), `pll` is the PLL frequency (in Hz) in the range of 600..900MHz, `f` is the
desired output frequency (in Hz). If `pll` is zero, then a suitable value will be
chosen.

The output will be values such that f0 * (a0 + b0/c0) / (a1 + b1/c1) to within as
small a tolerance as possible.

An error is returned if the routine cannot find good parameters for the dividers
or if the input is invalid.
*/
func New(f0, pll, f float64) (Si5351Config, error) {
	//if f < 3700 {
	//	return Si5351Config{}, errors.New("f too small")
	//}
	if f0 < 10e6 || f0 > 27e6 {
		return Si5351Config{}, errors.New("Si5351Config: invalid clock frequency")
	}

	if f > 200e6 {
		return Si5351Config{}, errors.New("Si5351Config: output frequency > 200MHz")
	}

	if f > 150e6 {
		pll = 4 * f
	} else if f >= 100e6 {
		pll = 6 * f
	} else if pll == 0 {
		if f < 5e6 {
			pll = 600e6
		} else {
			pll = 800e6
		}
	} else if pll < 600e6 || pll > 900e6 {
		return Si5351Config{}, errors.New("si5351Config: pll is out of range")
	}
	z := pll / f0
	if z < 15 {
		return Si5351Config{}, errors.New("Si5351Config: can't happen, feedback ratio too small")
	}
	if z > 90 {
		return Si5351Config{}, errors.New("Si5351Config: can't happen, feedback ratio too big")
	}
	b, c, _ := NearestFraction(uint64(z*1e12), 1_000_000_000_000, 1<<20)
	r := Si5351Config{
		f0:  f0,
		pll: pll,
		f:   f,
		a0:  uint32(b / c),
		b0:  uint32(b % c),
		c0:  uint32(c),
	}

	z = f0 * (float64(r.a0) + float64(r.b0)/float64(r.c0)) / f
	if !near(z, 4, 1e-9) && !near(z, 6, 1e-9) && z < 8 {
		return Si5351Config{}, fmt.Errorf("Si5351Config: output multi-synth ratio too small: %.5g %v", z-6, r)
	}
	r.r = 1
	for z/float64(r.r) > 2048 && r.r <= 128 {
		r.r = r.r * 2
	}
	if r.r > 128 {
		return Si5351Config{}, errors.New("Si5351Config: output divider ratio too big, f_out too low")
	}
	b, c, _ = NearestFraction(uint64(z*1e12/float64(r.r)), 1_000_000_000_000, 1<<20)
	r.a1 = uint32(b / c)
	r.b1 = uint32(b % c)
	r.c1 = uint32(c)

	r.f = f0 * (float64(r.a0) + float64(r.b0)/float64(r.c0)) / (float64(r.a1) + float64(r.b1)/float64(r.c1))
	if math.Abs(r.eps)/f > 1e-12 {
		return Si5351Config{}, errors.New("si5351Config: frequency error is out of range")
	}
	r.eps = f - r.f
	return r, nil
}

func near(a float64, b float64, eps float64) bool {
	return math.Abs(a-b) <= eps
}
