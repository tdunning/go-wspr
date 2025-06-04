# High precision WSPR

This directory contains software to implement a WSPR beacon on a Raspberry Pico.

The unusual/novel aspects of this software include:

a) disciplining the transmit frequency to GPS with very high precision (< 10
ppb)

b) implementing very fine frequency adjustments so that the transition from one
FSK frequency to another can be made smoothly, even at VHF (or even UHF)
frequencies. With this code, millihertz resolution at >100MHz is possible using
the Si5351. At higher frequencies, this is 3-5 orders of magnitude better than
with simpler approaches.

The first point is supported by a novel architecture using the PWM, PIO and DMA
capabilities to achieve true 32 bit frequency counting referenced to the PPS
output of a GPS module. The jitter on the period for this count is < 50ns and
there is no reset offset.

Both points are supported by the use of high precision rational arithmetic for
computing the settings on the Si5351 used to generate the transmitted frequency.

This repository includes some WIP related to support for the RP2350. That code
is in the src/machine_x directory and can safely be ignored if you are here for
the high resolution frequency synthesis.

# High resolution counting

In order to discipline a synthesized frequency generator, it is necessary
to first measure the current output frequency and then to adjust it. The
RP2040 (and RP2350) has built in PWM hardware that can act as a frequency
counter that can be used to measure frequencies but there are serious 
limitations. The most serious issues are that the PWM counters are only 
16 bits long, there isn't an easy way to sample the PWM counters from 
an external time reference and most software for sampling the PWM counters 
is subject to several microseconds of jitter in the response time even
if there are no surprises such as garbarge collection.

You can find other efforts to use the Pico as a frequency counter in 
[Richard Kendall's blog](https://rjk.codes/post/building-a-frequency-counter/), 
or in [Jeremy Bentham's work](https://iosoft.blog/2023/07/30/picofreq/). 
These other efforts use a variety of methods to solve the problem of
resolution, but generally still suffer from the problem of jitter.

The novelty in my efforts is in chaining PWMs together in hardware instead
of software to get high resolution. At the same time, I use the PIO and DMA 
systems of the Pico to get very stable, low-jitter values of microsecond time 
and counter state into a buffer without worrying about counter wraparound between 
samples.

## How the PWM hardware works

The RP2040 and RP2350 have a number of PWM cores that include a counter that
counts up to set value (known as TOP) and then either reverse or are reset
directly to zero. The clock input for these counters is typically the
system clock. That allows these counters can be used to generate pulse trains with
variable duty cycle by setting a threshold value (known as THRESH). While the
count is below the threshold, the output is low and when the count passes the
threshold the output is high. Each PWM core has two outputs, A and B, which
share the same counter, but which can have independent thresholds.

The clock for the counter normally comes from the internal processor clock which
runs at 133MHz in the RP2040 and at 150MHz for the RP2350. This internal clock
can be replaced by an external clock which is taken from the B input for the PWM
core. Either way, the clock can be scaled down if slower PWM outputs are desired.

Interestingly, the A *output* of a PWM core can still be used as an output when
the B is used as an input for the clock. This means that PWM cores can be
daisy-chained by connecting the A output from one PWM to the B input on the
next. This can be problematic in that there is a delay of several internal clock
periods from when the fast PWM is reset to when the slow PWM is incremented, but
there are ways to compensate for this effect. One compensation will be discussed later.
They key here is that the resolution of the counters can be extended 16 bits at
a time to truly absurd levels. Even with only 32 bits, a 100MHz input (which is
possible if you clock the CPU at over 200MHz) will take 42 seconds to roll over.
If we go for the absurd and use 48 bits, the same 100MHz counter won't roll over
for over a month.

So then, the question is how to read these composited PWM counters at a precise moment in
time and still retain as much accuracy while preserving all this lovely precision.

## Reading the counters

Reading the counters is harder than it looks. First, there is fact that we can only
read different counters one after another which means we physically can't read them at
exactly at the same time. That can lead to confusion during the moments when the
lower order counter is rolling over and incrementing the higher order counter because
we can't tell whether we are getting the value of the slow counter before or
after it is incremented.

We can, however, detect that a rollover has occurred, even in the presence of a 
short delay before the higher counter increments by reading the counters more than 
once. If B is the slow (higher order) counter and A is the fast (lower order) 
counter, we can read a sequence of values values B1 A1 B2 A2. Based on observation 
with live signals, the time between successive samples is roughly 75ns or about 
10 clock cycles at 133MHz. In contrast, the delay from the wrap-around of A 
and the incrementing of B is no more than half that, or about 5 cycles. That is
important because we know for certain that the slow counter can increment
at most once between measurements of the fast counter.

Because of the timings involved, we can determine a 32-bit count by breaking
things down into the following two cases:

- B1 = B2. No increment of B happened between B1 and A1 so (B1 A1) is a good
  measurement of the state at the time of the A1. Note that we *don't* know
  that B didn't increment between B1 and B2, but we *do* know that B could 
  only have incremented *after* B2-5 clocks and thus could only 
  have incremented after we measured A1 (if it incremented at all) since A1 
  is at about B2-10 clocks.

- B1 = B2-1. The logic here is tricky because B may have incremented before or
  after A1. If B incremented between B1 and A1, then (B2, A1) is the correct
  thing to return. If B incremented between A1 and B2, however, then (B1, A1) is
  the best answer. We can distinguish these two sub-cases by examining A1 and A2.
  If A1 is near 0xffff and A2 is near zero, then A rolled over *after* A1 and B
  could only have incremented after that so the answer is (B1, A1). On the other
  hand, if A1 <= A2, then both A1 and A2 were read after the increment of B so 
  the answer is (B2, A1). 

The upshot is that we can read the counters and get the value of both B and A as of 
the time of the A1 sample. The remaining problem is how can place that A1 sample 
in time as accurately as possible? The basic answer is to this same trick to read
the high and low portions of the fast system clock on the Pico immediately before
or after reading the state of the counter. If the time between these two measurements
can have very low jitter, then we know the value at a precise moment in time.

Thus, the remaining problem is to control this measurement jitter and the jitter
between an external event and the measurement.

## Controlling sample time jitter

The answer to controlling jitter in the sample time of A1 is to use the
specialized hardware of the RP2040 to trigger and conduct the entire sampling 
process without any software intervention. One way to get all of the required
samples in a clean sequence is to use DMA chaining. In this process, one DMA
channel reads a memory buffer containing control blocks that, in turn, set
the control registers in a second DMA channel. Those control blocks each contain a
source and destination address which, when written to second DMA, will cause a
32 bit value to be transfered. After that transfer, the second DMA triggers the
first DMA to write another control block. That cause anothers transfer and 
another trigger until a null control block is encountered. In the system built 
here, the control blocks read from the following locations:

```
TIMERAWH
TIMERAWL   <-- t1
TIMERAWH
TIMERAWL
PWM1.CTR
PWM0.CTR   <-- t2
PWM1.CTR
PWM0.CTR
```

This lets us reconstruct a microsecond timestamp at time `t1` and to get 
a sample of the composite counter at time `t2` roughly 40 clocks later 
(actually 300±15ns). The DMA system transfers data at a 48MHz tempo with 
three transfers required for each value above.

The remaining problem then boils down to triggering this DMA transfer from
an external signal with a constant delay with very low jitter. This can't be
done in software because that introduces a massive amount of jitter. This also
can't be done using the DMA hardware alone because a GPIO can't be used as
a data read trigger on a DMA channel. The solution is to use a short PIO
program and a third DMA channel. The PIO program monitors the input pin that the
GPS is driving once per second. Within one system clock after a rising
transition is detected, the PIO writes a value to its FIFO and a third DMA
channel transfers that value from the PIO to the transfer count of the first
DMA, triggering the transfer process.

In testing a live system on a 30MHz input, the resulting counts appear to
indicate that the sample initiation process is stable to within 30ns. Over a 10
second period, this means that we should be able to get 3 ppb jitter for this
30 MHz input which means counting errors will be limited only by the size of 
the count. For 30MHz-50MHz, this gives us about 100 mHz error. This method is 
limited on the Raspberry Pico to measuring frequencies less than roughly 50MHz, 
but if we use Si5351 clock generator, we can generate multiple signals from 
the same internal or external oscillator. That let's us generate a 40MHz signal
for calibrating the oscillator in the SI5351 and transfer that calibration to
a separate signal at a higher frequency. We can also use harmonics of the 
SI5351 output to generate disciplined signals above the 200MHz frequency limit
of the SI5351. All together, this system allows signal generation in the 
2m (144-148MHz), 1.25m (220-222MHz) and 70cm (420-450MHz) bands with 200-600mHz 
resolution with high accuracy and stability.

# Turning frequency measurements into frequency control

Once we get an accurate and high-resolution frequency measurement, the question
becomes one of whether we can control a frequency generator precisely enough to
get precisely the output we want.

The system described here uses an Si5351 clock generator to generate the
transmitted signal. This chip generates a (nearly) square-wave signal on each of
three outputs. These outputs can be driven from either of two internal
phase-locked-loops (PLL) via a integer + fractional divider that has 20 bit
resolution for the numerator and denominator in the divider. The PLLs can 
be configured to run in the range from 600-900MHz referenced to an integrated 
crystal reference that runs in this design at 25MHz via a similar 
integer + fractional divider. The output frequency can be set by
changing the frequency of the PLL or by changing the divider after the PLL, but
the question of resolution applies equally. At frequencies above 50MHz, phase
noise is lower if you control the PLL frequency and set the divider to an even
integer value.

It is common in HF band WSPR beacons based on the Si5351 to set the denominator 
of the frequency divider to a fixed value chosen to allow stepping the generated 
frequency by exactly the required WSPR tone separation. For example, to transmit 
on the 30m band, the PLL can be set to 900MHz and the denominator can be set to 
the somewhat magical number 778730 to get frequency spacing that is very close 
to 1/10 of the nominal 1.4648Hz separation in the WSPR specification. This 
works, and it even allows for the generated tone to be moved smoothly from 
one tone to the next rather than jumping it directly to the next value. In 
the 10m band, however, this method can produce steps no finer than the full 
channel spacing making the microtone adjustments from the WSPR spec impossible. 
At higher frequencies, this method doesn't even allow steps as small as the 
tone spacing. In any case, compensation for small errors in oscillator frequency
is also not possible.

This problem is illustrated in the following table which shows the integer and
fractional settings on the divider (a, b and c, respectively) and the resulting
output frequencies with a PLL at 900MHz.

| a  | b | c      | Frequency (Hz) | Δf (Hz) |
|----|---|--------|----------------|---------|
| 88 | 2 | 778730 | 10,227,272.429 | 0.0     |
| 88 | 1 | 778730 | 10,227,272.578 | 0.149   | 
| 88 | 0 | 778730 | 10,227,272.727 | 0.298   |
| 32 | 2 | 600000 | 28,124,997.07  | 0.0     |
| 32 | 1 | 600000 | 28,124,998.54  | 1.47    |
| 32 | 0 | 600000 | 28,125,000.00  | 2.93    |

None of the WSPR bands above 10m can be addressed by this mechanism and anywhere
above about 10MHz, the solution is marginal.

## Number theory to the rescue

There is a way to fix this problem of resolution at higher frequencies. The
trick is to vary both numerator and denominator to control the PLL frequency.
The specific values for the numerator and denominator can be chosen using 
continued fractions to find an optimal rational approximation of our 
desired value. This is the same method that is used by Glenn Elmore to produce
[high quality disciplined oscillators](http://www.sonic.net/~n6gn/OSHW/AB/RE/Reference2.html).

It would appear at first that simply setting the denominator (c) to as large a
value as possible would give the finest resolution, but this is not true. The
following table shows how this works.

| a  | b      | c      | Frequency (Hz) | Δf (Hz) |
|----|--------|--------|----------------|---------|
| 31 | 15611  | 31250  | 28124600.00000 | 0.00000 |
| 31 | 311219 | 622996 | 28124600.14648 | 0.14648 |
| 31 | 111031 | 222261 | 28124600.29296 | 0.29296 |
| 31 | 302517 | 605576 | 28124600.43944 | 0.43944 |

The values of b and c are now seemingly chosen at random, but the frequencies
generated are spot on the desired values.

This same approach can work in the 2m band

| a  | b      | c      | Frequency (Hz)  | Δf (Hz) |
|----|--------|--------|-----------------|---------|
| 34 | 16943  | 25000  | 144490500.00000 | 0.00000 |
| 34 | 97938  | 144511 | 144490500.14647 | 0.14647 |
| 34 | 425657 | 628072 | 144490500.29296 | 0.29296 |
| 34 | 89701  | 132357 | 144490500.43947 | 0.43947 |

The precision here is kind of amazing. Using this method, we can control the
output with a resolution of 0.1mHz. This is 12 significant digits which seems
like the result of dark arts.

So how can these seemingly magical values be determined? The algorithm for 
finding these values of a, b and c is actually fairly simple.

The fundamental idea is based on continued fractions as a way to find the best
fractional representation of a number with a limited size for the denominator.
To generate a frequency $f = 144,490,500.14647Hz$, we figure out the required up
conversion for the PLL given a fixed down conversion of 6x. This gives a PLL
frequency of about 867MHz and which is about 34.68 times the 25MHz clock on the
generator. 

Skipping to the final answer, this reduces to a call to `NearestFraction` with the following
parameters

```go
NearestFraction(34_677_720_035_152, 1_000_000_000_000, 1<<20)
```

This is asking for the closest approximation of the slightly outrageous fractional
representation of our intended factor 34_677_720_035_152/1_000_000_000_000 
with a denominator in the result no larger than 2^20.

Inside the `NearestFraction` function, we rewrite the desired fraction $a/b$ using
$$
a/b = \lfloor a / b \rfloor + {a \bmod b} / b
$$
We abbreviate this a little bit by setting $k_1 = \lfloor a / b \rfloor$ and $a_1 = {a \bmod b}$
$$
a/b &= k_1 + a_1 / b 
$$
At this point, we know that $a_1 \lt b$ so we can repeat this trick by expanding
$b/a_1$ using $k_2 = \lfloor b/a_1 \rfloor$ and $b_1 = b \bmod a_1$
$$
a/b = k_1 + \frac 1 {k_2 + b_1 / a_1}
$$
But $b_1 \lt a_1$ do this again (and again and again!) with $k_3 = \lfloor a_1 / b_1 \rfloor$ 
and $a_2 = a_1 \bmod b_1$
$$
a/b = k_1 + \frac 1 {k_2 + \frac 1 {k_3 + a_2/b_1}}
$$
This seems like busywork, but the cool thing is that the continued fractions formed
from more and more elements of the sequence of $k_1, k_2, k_3 \ldots$ are the best possible
fractional approximations of $a/b$ with larger and larger denominations (strictly speaking
we may need to increment the last $k_n$ to get the best value). All we have to do is
to know when to stop. The function `NearestFraction` continues this process until going one 
more step would result in too large a denominator.

For the ratio that we started with at the beginning of this section, if we apply this process
we get a sequence $k_1 \ldots k_9 = (34, 1, 2, 9, 1, 2, 1, 1, 4)$ that represents the fraction
$33464/965$. This is only $1.72 \times 10^-7$ away from the correct value which is impressive
since the denominator is less than a thousand.
