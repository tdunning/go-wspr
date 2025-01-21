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

import "testing"

func Test_timing(t *testing.T) {
	scale := uint64(0x10000)
	type testCase struct {
		args []uint32
		r    uint64
	}
	var tests = []testCase{
		{
			[]uint32{100, 40, 100, 45, 100}, 100*scale + 40,
		},
		{
			[]uint32{100, 0xfff5, 100, 45, 101}, 100*scale + 0xfff5,
		},
		{
			[]uint32{100, 0xfff5, 101, 45, 101}, 100*scale + 0xfff5,
		},
		{
			[]uint32{100, 40, 101, 45, 101}, 101*scale + 40,
		},
	}

	for _, test := range tests {
		v := ReduceObservation(scale, test.args[0], test.args[1], test.args[2], test.args[3])
		if v != test.r {
			t.Errorf("timing(%d, %d, %d, %d, %d) = %d, want %d",
				scale, test.args[0], test.args[1], test.args[2], test.args[3], v, test.r)
		}
	}
}
