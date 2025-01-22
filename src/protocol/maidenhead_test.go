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

import "testing"

func TestMaidenhead(t *testing.T) {
	tests := []struct {
		name string
		lat  float64
		long float64
		loc  string
	}{
		{"W1AW", 41.7148, -72.7272, "FN31PR"},
		{"St Kilda Pier", -37.864701, 144.966135, "QF22LD52"},
		{"Gulf of Guinea", 0, 0, "JJ00AA00"},
		{"The pole", 89.997743, 179.995486, "RR99XX99"},
		{"K6BYT", 37.332445, -122.128147, "CM87"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mh := Maidenhead(test.lat, test.long)
			if mh[0:len(test.loc)] != test.loc {
				t.Errorf("got %s, want %s", mh, test.loc)
			}
		})
	}
}
