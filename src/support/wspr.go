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

/*
This file contains code that implements the WSPR protocol
*/

package support

import (
	"errors"
)

func WsprMessage(callsign, location string, power int) ([]int8, error) {
	bits, err := PackBits(callsign, location, power)
	if err != nil {
		return nil, err
	}
	message := Parity(bits)
	message = interleave(message)
	for i := 0; i < len(message); i++ {
		message[i] = 2*message[i] + sync[i]
	}
	return message, nil
}

/*
interleave copies the values in the message by bit-reversing the index of the
source to get a destination position. Destination positions that are outside the
destination vector are ignored and the next one is used. That makes this
operation somewhat different from the normal mathematical definition where
elements with invalid destinations are simply left in place. That change also
makes this rearrangement frustratingly non-involutional, so it can't be done in
place.
*/
func interleave(message []int8) []int8 {
	n := len(message)
	dest := make([]int8, len(message))
	si := 0
	for i := 0; i < 255; i++ {
		ix := reverseByte(i)
		if ix < n {
			dest[ix] = message[si]
			si++
			if si >= len(message) {
				return dest
			}
		}
	}
	return nil
}

var sync = []int8{
	1, 1, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 1, 1, 1, 0, 0, 0, 1, 0,
	0, 1, 0, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 1, 0, 1,
	0, 0, 0, 0, 0, 0, 1, 0, 1, 1, 0, 0, 1, 1, 0, 1, 0, 0, 0, 1,
	1, 0, 1, 0, 0, 0, 0, 1, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 0, 1,
	0, 0, 1, 0, 1, 1, 0, 0, 0, 1, 1, 0, 1, 0, 1, 0, 0, 0, 1, 0,
	0, 0, 0, 0, 1, 0, 0, 1, 0, 0, 1, 1, 1, 0, 1, 1, 0, 0, 1, 1,
	0, 1, 0, 0, 0, 1, 1, 1, 0, 0, 0, 0, 0, 1, 0, 1, 0, 0, 1, 1,
	0, 0, 0, 0, 0, 0, 0, 1, 1, 0, 1, 0, 1, 1, 0, 0, 0, 1, 1, 0, 0, 0,
}

/*
PackBits encodes a WSPR message as bits packed from the compressed form of
the callsign, location and power. These resulting bits are in bits number 55...6
which is the WSPR convention.

An error is returned if there is an error encoding anything.
*/
func PackBits(callsign, location string, power int) (uint64, error) {
	c, err := CallSign(callsign)
	if err != nil {
		return 0, err
	}
	l, err := Locator(location)
	if err != nil {
		return 0, err
	}
	return (c << 28) + (l << 13) + (Power(power) << 6), nil
}

func Parity(message uint64) []int8 {
	// these hold the 162 bits of output, one bit per byte
	out := make([]int8, 162)
	// output counter
	k := 0
	s1 := uint32(0)
	s2 := uint32(0)
	for i := 55; i >= 6; i-- {
		bit := (message >> i) & 1
		s1 = (s1 << 1) | uint32(bit)
		out[k] = int8(parity32(s1 & 0xF2D05351))
		k++

		s2 = (s2 << 1) | uint32(bit)
		out[k] = int8(parity32(s1 & 0xE4613C47))
		k++
	}
	// these bottom bits are all zeros
	for i := 0; i < 31; i++ {
		s1 = s1 << 1
		out[k] = int8(parity32(s1 & 0xF2D05351))
		k++

		s2 = s2 << 1
		out[k] = int8(parity32(s1 & 0xE4613C47))
		k++
	}
	return out
}

func reverseByte(in int) int {
	in = (in&0xF0)>>4 | (in&0x0F)<<4
	in = (in&0xCC)>>2 | (in&0x33)<<2
	in = (in&0xAA)>>1 | (in&0x55)<<1
	return in
}

func parity32(input uint32) int {
	input = input ^ (input >> 1)
	input = input ^ (input >> 2)
	input = input ^ (input >> 4)
	input = input ^ (input >> 8)
	input = input ^ (input >> 16)
	return int(input & 1)
}

/*
CallSign converts a 5 or 6 character call sign to a uint64 using the very
idiosyncratic WSPR encoding. The call sign must match the regex
`[A-Z0-9]?[A-Z0-9][0-9][A-Z ][A-Z ][A-Z ]`.
*/
func CallSign(callsign string) (uint64, error) {
	var err error
	var encoded uint64
	var encodeCount int
	tail := 0
	n := len(callsign)
	if n >= 2 && isdigit(callsign[1]) {
		// for example K1ABC
		encoded, err = encodeChar(' ', ALPHA|DIGIT|SPACE, 0)
		if err != nil {
			return 0, err
		}
		encoded, err = encodeChar(callsign[0], ALPHA|DIGIT, encoded)
		if err != nil {
			return 0, err
		}
		encoded, err = encodeChar(callsign[1], DIGIT, encoded)
		if err != nil {
			return 0, err
		}
		tail = 2
	} else if n >= 3 && isdigit(callsign[2]) {
		encoded, err = encodeChar(callsign[0], ALPHA|DIGIT|SPACE, 0)
		if err != nil {
			return 0, err
		}
		encoded, err = encodeChar(callsign[1], ALPHA|DIGIT, encoded)
		if err != nil {
			return 0, err
		}
		encoded, err = encodeChar(callsign[2], DIGIT, encoded)
		if err != nil {
			return 0, err
		}
		tail = 3
	} else {
		return 0, errors.New("ill-formed callsign, must start with {alpha}{alpha}?{digit}")
	}
	encodeCount = 3
	for ; tail < len(callsign); tail++ {
		encoded, err = encodeChar(callsign[tail], ALPHA|SPACE, encoded)
		encodeCount++
	}
	for ; encodeCount < 6; encodeCount++ {
		encoded, err = encodeChar(callsign[tail], ALPHA|SPACE, encoded)
		encodeCount++
	}
	return encoded, nil
}

/*
Locator converts a four character Maidenhead location reference into a uin64
using an interleaved encoding
*/
func Locator(locator string) (uint64, error) {
	if len(locator) != 4 {
		return 0, errors.New("locator has wrong length")
	}
	encoded, err := encodeChar(locator[0], MAIDENHEAD, 0)
	if err != nil {
		return 0, err
	}
	encoded, err = encodeChar(locator[2], DIGIT, encoded)
	if err != nil {
		return 0, err
	}
	encoded = 179 - encoded
	encoded, err = encodeChar(locator[1], MAIDENHEAD, encoded)
	if err != nil {
		return 0, err
	}
	encoded, err = encodeChar(locator[3], DIGIT, encoded)
	if err != nil {
		return 0, err
	}
	return encoded, nil
}

/*
Power encodes a power measured in dBm to the required bits
*/
func Power(dBm int) uint64 {
	return uint64(0x40 + dBm)
}

func isdigit(b byte) bool {
	return b >= '0' && b <= '9'
}

const (
	ALPHA int = 1 << iota
	DIGIT
	SPACE
	MAIDENHEAD
)

func encodeChar(c byte, legalValues int, encoded uint64) (uint64, error) {
	v := uint64(1000)
	offset := uint64(0)
	if legalValues&DIGIT != 0 {
		if '0' <= c && c <= '9' {
			v = offset + uint64(c) - '0'
		}
		offset += 10
	}
	if legalValues&ALPHA != 0 {
		if 'A' <= c && c <= 'Z' {
			v = offset + uint64(c) - 'A'
		} else if 'a' <= c && c <= 'z' {
			v = offset + uint64(c) - 'a'
		}
		offset += 26
	}
	if legalValues&MAIDENHEAD != 0 {
		if legalValues&(DIGIT|ALPHA|SPACE) != 0 {
			return 0, errors.New("Illegal character spec, can't combine Maidenhead with other classes")
		}
		offset += 18
		if 'A' <= c && c <= 'R' {
			v = uint64(c) - 'A'
		} else if 'a' <= c && c <= 'r' {
			v = uint64(c) - 'a'
		}
	}
	if legalValues&SPACE != 0 {
		if legalValues&(DIGIT|ALPHA) == 0 {
			return 0, errors.New("Illegal character spec, can't have space without ALPHA or DIGIT")
		}

		if c == ' ' {
			v = offset
		}
		offset++
	}
	if v == 1000 {
		return 0, errors.New("invalid character in callsign")
	}
	return encoded*offset + v, nil
}

/*
0 dBm = 0.001 W
 3 0.002
 7 0.005
10 0.01
13 0.02
17 0.05
20 0.1
23 0.2
27 0.5
30 1
33 2
37 5
40 10
43 20
47 50
50 100
53 200
57 500
60 1000
*/
