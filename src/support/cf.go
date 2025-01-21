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

/*
Finds the best approximation c/d ≈ a/b such that d < max_denominator

Returns c, d and the error a/b - c/d as floating point.

The method used is by creating terms of a continued fraction until the denominator
of the rational value of the confinued fraction would be too big.

As a concrete example, suppose you want to divide an 900MHz signal down using a
divider of the form a+b/c where c < 2^20 to get the 4 frequencies separated by
1.4648 Hz starting at 144.490 MHz. Just setting c to 2^20-1 gives you four identical
frequencies which is useless. Using nearest fraction gives correct answers to within
half a milli-hertz. Even at a lower frequency of 28.126_000 kHz, there are errors with
the fixed divisor of nearly 0.5 Hz. Since WSPR uses continuous variation of frequency
at a very fine level, this kind of quantization error will degrade the ability to pick
out signals from noise. Using the nearest fraction, on the other hand, gives accuracies
at much finer than millihertz.

Another example, also from radio, involves compensating for frequency drift by measuring
the generated signal against a reference such as a GPS receivers pulse-per-second output.
As the frequency drifts, the value of the drift can be included in the computation of
frequency divisors. With nearestFraction, you can still get millihertz accuracy which
allows 10 ppb frequency stability relative to your reference even up into the VHF.

The precision you can get varies depending on the frequency you are targeting. To get
this high precision at all frequencies, you may need to be able to adjust the source
frequency which is easily done with most frequency generators like the Si5351.
*/

func NearestFraction(a, b, max_denominator uint64) (c, d uint64, eps float64) {
	c, d = continuedFraction(a, b, 0, 1, max_denominator)
	eps = float64(a)/float64(b) - float64(c)/float64(d)
	return c, d, eps
}

/*
Finds a continued fraction approximation for a/b. Returns the rational value
of the continued fraction expressed as two integers.

We compute the continued fraction recursively. Any rational a/b can be written
as

	cf(a, b) = floor(a/b) + rem(a/b) / b

But that second term can be inverted so we have

	cf(a, b) = floor(a/b) + 1 / cf(b, rem(a/b))

It isn't obvious, but these continued fractions approximations are the best
rational approximations for the resulting denominator. The only tricks left is
when to quit and how to compute the rational representation as we back out of
the recursion. We decide to terminate when the denominator would exceed our limit,
but in order to know that, we have to accumulate two extra numbers e, f which should
start at 1 and 0, respectively.
*/
func continuedFraction(a, b, e, f, max_denominator uint64) (c, d uint64) {
	term := a / b
	denom := f + term*e
	if denom > max_denominator {
		return 1, 0
	} else {
		ax := a - term*b
		// a / b = term + ax / b
		if ax == 0 {
			return term, 1
		} else {
			// a / b = term + ax/b = term + 1 / cf(b, ax),
			// cx/dx = cf(b, ax)
			// a / b = term + dx / cx = (term*cx + dx) / cx
			cx, dx := continuedFraction(b, ax, denom, e, max_denominator)
			return term*cx + dx, cx
		}
	}
}
