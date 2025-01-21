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
	"math"
	"testing"
)

var seed = int64(1)

func rand() float64 {
	seed = 25214903917*seed + 11
	return float64(seed&0xffff_ffff_ffff) / float64(1<<48)
}

func Test_accuracy(t *testing.T) {
	frequencies := [][]float64{ // multiple test bands
		{1838000, 1838200},
		{3570000, 3570200},
		{5288600, 5288800},
		{7040000, 7040200},
		{10140100, 10140300},
		{14097000, 14097200},
		{18106000, 18106200},
		{21096000, 21096200},
		{24926000, 24926200},
		{28126000, 28126200},
		{50294400, 50294600},
		{144489900, 144490100},
	}
	for i := 0; i < len(frequencies); i++ {
		for f := frequencies[i][0]; f <= frequencies[i][1]; f += rand() * 0.2 {
			config, err := New(25e6, 0.0, f)
			if err != nil {
				t.Errorf("Error in si5351Config: %s", err)
			}
			if math.Abs(config.eps)/f > 1e-9 {
				t.Errorf("Big discrepancy: %.4f, %.2f vs %.2f", config.eps, config.f, f)
			}
		}
	}
}

func Test_range(t *testing.T) {
	for f := 1.0; f < 2300; f += 50 {
		_, err := New(25e6, 0.0, f)
		if err == nil {
			t.Errorf("Expected error in si5351Config due to low frequency: %.3f", f)
		}
	}
	for f := 2302.0; f < 200e6; f *= 1.2 {
		r, err := New(25e6, 0.0, f)
		if err != nil {
			t.Errorf("Error in si5351Config: %s", err)
		}
		if r.eps > 1e-3 {
			t.Errorf("Error in si5351Config: %.3f", r.eps)
		}
	}
}
