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
	"math/big"
	"testing"
)

func Test_continuedFraction(t *testing.T) {
	type args struct {
		a, b, e, f, max_denominator uint64
	}
	tests := []struct {
		name  string
		args  args
		wantC uint64
		wantD uint64
	}{
		{
			name:  "base case",
			args:  args{a: 10, b: 1, e: 1, max_denominator: 100},
			wantC: 10,
			wantD: 1,
		},
		{
			name:  "null case",
			args:  args{a: 0, b: 1, e: 1, max_denominator: 100},
			wantC: 0,
			wantD: 1,
		},
		{
			name:  "exact division",
			args:  args{a: 63, b: 9, e: 0, f: 1, max_denominator: 10},
			wantC: 7,
			wantD: 1,
		},
		{
			name:  "exact answer",
			args:  args{a: 23, b: 5, e: 0, f: 1, max_denominator: 10},
			wantC: 23,
			wantD: 5,
		},
		{
			name:  "limited depth",
			args:  args{a: 2300, b: 500, e: 0, f: 1, max_denominator: 7},
			wantC: 23,
			wantD: 5,
		},
		{
			name:  "less limited depth",
			args:  args{a: 2301, b: 500, e: 0, f: 1, max_denominator: 97},
			wantC: 23,
			wantD: 5,
		},
		{
			name:  "almost unlimited depth",
			args:  args{a: 451, b: 98, e: 0, f: 1, max_denominator: 99},
			wantC: 451,
			wantD: 98,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotC, gotD := continuedFraction(tt.args.a, tt.args.b, tt.args.e, tt.args.f, tt.args.max_denominator)
			if gotC != tt.wantC {
				t.Errorf("continuedFraction() gotC = %v, want %v", gotC, tt.wantC)
			}
			if gotD != tt.wantD {
				t.Errorf("continuedFraction() gotD = %v, want %v", gotD, tt.wantD)
			}
		})
	}
}

func Test_limit(t *testing.T) {
	maxD := [][]uint64{
		{5, 3, 1},
		{7, 22, 7},
		{10, 22, 7},
		{105, 22, 7},
		{106, 333, 106},
		{110, 333, 106},
		{113, 355, 113},
		{1000, 355, 113},
		{10000, 355, 113},
		{33000, 355, 113},
		{33102, 103993, 33102},
		{33200, 103993, 33102},
		{33215, 104348, 33215},
		{50000, 104348, 33215},
		{100_000, 312689, 99532},
		{100_000_000, 219684069, 69927611},
		{100_000_000_000, 157079632679, 50000000000},
	}
	lastD := uint64(0)
	last := big.NewRat(10, 1)
	var eps big.Rat
	for i := 0; i < len(maxD); i++ {
		// residual should be monotonic decreasing
		a, b := continuedFraction(314159265358, 100_000_000_000, 0, 1, maxD[i][0])
		// the residual is hard to compute in floating point ... switch to big rationals
		eps := eps.Abs(eps.Sub(big.NewRat(int64(a), int64(b)), big.NewRat(314159265358, 100_000_000_000)))
		if b != lastD && new(big.Rat).Sub(last, eps).Num().Int64() < 0 {
			t.Errorf("at %d, error increased from %s to %s", maxD[i][0], last.FloatString(10), eps.FloatString(10))
		}
		last.Set(eps)
		lastD = b

		// and result should step to next denominator in good order
		if a != maxD[i][1] || b != maxD[i][2] {
			t.Errorf("%d => %d, %d, but wanted %v", maxD[i][0], a, b, maxD[i][1:])
		}
	}
}

func Test_nearest_fraction_wspr_frequencies(t *testing.T) {
	f_table := []float64{144_490_000.0, 28_125_000.0}
	df := 1.4648
	top := 900e6
	max_denominator := uint64((1 << 20) - 1)

	for j := 0; j < 2; j++ {
		f0 := f_table[j]
		for i := 0; i < 4; i++ {
			f := f0 + float64(i)*df
			r := top / f
			b, c, _ := nearest_fraction(uint64(math.Round(r*140e9)), uint64(140e9), max_denominator)
			f1 := top * float64(c) / float64(b)
			a := b / c
			b = b - a*c
			if math.Abs(f1-f) > 1e-3 {
				t.Errorf("excessive error: Δf = %.3f", f1-f)
			}
		}
	}
}

func Test_nearest_fraction(t *testing.T) {
	type args struct {
		a               uint64
		b               uint64
		max_denominator uint64
	}
	tests := []struct {
		name    string
		args    args
		wantC   uint64
		wantD   uint64
		wantEps float64
	}{
		{"exact division", args{a: 3879 * 1712, b: 1712, max_denominator: 20}, 3879, 1, 0},
		{"famous pi", args{a: uint64(math.Round(math.Pi * 3879)), b: 3879, max_denominator: 100}, 22, 7, math.Round(math.Pi*3879)/3879 - 22.0/7.0},
		{"famous pi, larger", args{a: uint64(math.Round(math.Pi * 3879)), b: 3879, max_denominator: 110}, 333, 106, math.Round(math.Pi*3879)/3879 - 333.0/106.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotC, gotD, gotEps := nearest_fraction(tt.args.a, tt.args.b, tt.args.max_denominator)
			if gotC != tt.wantC {
				t.Errorf("nearest_fraction() gotC = %v, want %v", gotC, tt.wantC)
			}
			if gotD != tt.wantD {
				t.Errorf("nearest_fraction() gotD = %v, want %v", gotD, tt.wantD)
			}
			if gotEps != tt.wantEps {
				t.Errorf("nearest_fraction() gotEps = %v, want %v", gotEps, tt.wantEps)
			}
		})
	}
}
