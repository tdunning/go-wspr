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

import "device/rp"

func MicroTime() uint64 {
	tx := rp.TIMER
	th1, tl1, th2, tl2 := tx.TIMERAWH.Get(), tx.TIMERAWL.Get(), tx.TIMERAWH.Get(), tx.TIMERAWL.Get()
	return ReduceObservation(1<<32, th1, tl1, th2, tl2)
}

func ReduceObservation(scale uint64, th1 uint32, tl1 uint32, th2 uint32, tl2 uint32) uint64 {
	var t0 uint64
	if th1 == th2 {
		t0 = uint64(th1)*scale + uint64(tl1)
	} else {
		t0 = uint64(th2)*scale + uint64(tl2)
	}
	return t0
}
