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

package protocol

import (
	"math"
)

/*
This file contains support for foward and reverse encoding of lat-long pairs
into the Maidenhead representation.
*/

func Maidenhead(lat, long float64) string {
	lat += 90

	long += 180
	for long < 0 {
		long += 360
	}
	for long > 360 {
		long -= 360
	}

	code := [8]byte{}

	code[0] = 'A' + byte(int(long/20))
	code[1] = 'A' + byte(int(lat/10))

	long = math.Mod(long, 20)
	lat = math.Mod(lat, 10)
	code[2] = '0' + byte(int(long/2))
	code[3] = '0' + byte(int(lat))

	long = math.Mod(long, 2) * 24
	lat = math.Mod(lat, 1) * 24
	code[4] = 'A' + byte(int(long/2))
	code[5] = 'A' + byte(int(lat))

	long = math.Mod(long, 2) * 10
	lat = math.Mod(lat, 1) * 10
	code[6] = '0' + byte(int(long/2))
	code[7] = '0' + byte(int(lat))

	return string(code[0:])
}
