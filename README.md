# High precision WSPR

This directory contains software to implement a WSPR beacon on a Raspberry Pico.

The unusual/novel aspects of this software include:

a) disciplining the transmit frequency to GPS with very high precision (< 10
ppb)

b) implementing very fine frequency adjustments so that the transition from one
FSK frequency to another can be made smoothly, even at VHF (or even UHF)
frequencies. Millihertz resolution at >100MHz is possible through better
implementation.

The first point is supported by a novel architecture using the PWM, PIO and DMA
capabilities to achieve true 32 bit frequency counting referenced to the PPS
output of a GPS module. The jitter on the period for this count is < 50ns and
there is no reset offset.

Both points are supported by the use of high precision rational arithmetic for
computing the settings on the Si5351 used to generate the transmitted frequency.

# High resolution precision counting

In order to discipline a synthesized frequency generator, it is first necessary
to determine measure what the output frequency and then to adjust it. The
RP2040 (and RP2350) has built in PWM hardware that can act as a frequency
counter but there are serious limitations. The most serious issues are that the
PWM counters are only 16 bits long, there isn't an easy way to sample the PWM
counters from an external time reference and most software for sampling the PWM
counters is subject to several microseconds of jitter in the response time even
if there are no surprises such as garbarge collection.

## How the PWM hardware works

The RP2040 and RP2350 have a number of PWM cores that include a counter that
counts up to set value (known as TOP) and then either reverse or are reset
directly to zero. These counters can be used to generate pulse trains with
variable duty cycle by setting a threshold value (known as THRESH). While the
count is below the threshold, the output is low and when the count passes the
threshold the output is high. Each PWM core has two outputs, A and B, which
share the same counter, but which can have independent thresholds.

The clock for the counter normally comes from the internal processor clock which
runs at 133MHz in the RP2040 and at 150MHz for the RP2350. This internal clock
can be replaced by an external clock which is taken from the B input for the PWM
core. Either way, the clock can be scaled down if slower PWM outputs are desired
or so that the PWM operation can have maximum resolution.

Interestingly, the A *output* of a PWM core can still be used as an output when
the B is used as an input for the clock. This means that PWM cores can be
daisy-chained by connecting the A output from one PWM to the B input on the
next. This can be problematic in that there is a delay of several internal clock
periods from when the fast PWM is reset to when the slow PWM is incremented, but
there are some ways to compensate for this effect that will be discussed later.
They key here is that the resolution of the counters can be extended 16 bits at
a time to truly absurd levels. Even with only 32 bits, a 100MHz input (which is
possible if you clock the CPU at over 200MHz) will take 42 seconds to roll over.
If we go for the absurd and use 48 bits, the same 100MHz counter won't roll over
for over a month.

So then, the question is how to read these PWM counters and still retain as much
accuracy as we now have as lovely precision.

## Reading the counters

Reading the counters is harder than it looks. First, there is fact that we can't
read the counters exactly at the same time. That can lead to confusion when the
lower order counter is spilling over and incrementing the higher order counter.
We can detect this, even in the presence of a short delay before the higher
counter increments by reading the counters more than once. If B is the slow (
higher order) counter and A is the fast (lower order) counter, we can read
values B1 A1 B2 A2. Based on observation with live signals, the time between
successive samples is about 75ns which is about 10 clock cycles at 133MHz. In
contrast, the delay from the wrap-around of A and the incrementing of B is no
more than about 5 cycles.

Because of the timings involved, we can determine a 32-bit count as follows

- B1 = B2. No increment of B happened between B1 and A1 so (B1 A1) is a good
  measurement. This is because B could only have incremented *after* B2-5 clocks
  and thus could only have been after A1 (if at all) since A1 is at about B2-10.

- B1 = B2-1. The logic here is tricky because B may have incremented between B1
  and A1 making (B2, A1) the best answer or between A1 and B2 making (B1, A1)
  the best answer. If A1 is near 0xffff and A2 is near zero, A1 came before the
  increment of B so the answer is (B1, A1). Note that A2 is definitely after the
  increment, so it will be small. If A1 <= A2, then A1 also came after the
  increment of B so the answer is (B2, A1)

So we can read the counters and get the value of both B and A at the time of the
A1 sample, but how can we make that A1 sample as stable as possible?

## Controlling sample time jitter

The answer to controlling jitter in the sample time of A1 is to use the
specialized hardware of the RP2040 to trigger the entire sampling process
without any software intervention. One clean way to get all of the required
samples in a clean sequence is to use DMA chaining. In this process, one DMA
channel reads a memory buffer containing control blocks to reset control
registers in the second DMA channel. Those control blocks will each contain a
source and destination address which, when written to second DMA, will cause a
32 bit value to be transfered. After that transfer, the second DMA triggers the
first again to cause another transfer and another trigger until a null control
block is encountered. In the system built here, the control blocks read from the
following locations:

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

This lets us reconstruct a microsecond timestamp `t1` and get a sample of the
composite counter at `t2` about 40 clocks later (about 300ns). The DMA system
transfers data at a 48MHz tempo with three transfers required for each value
above.

The remaining problem then boils down to triggering this DMA transfer at a
precise time. This can't be done directly because the data read trigger on the
DMA can't be connect to GPIO directly. The solution is to use a short PIO
program and a third DMA channel. The PIO program monitors the input pin that the
GPS is driving once per second. Within one system clock after a rising
transition is detected, the PIO writes a value to the receive FIFO and the third
DMA transfers that value from the PIO to the transfer count of the first DMA,
triggering the transfer process.

In testing a live system on a 30MHz input, the resulting counts appear to
indicate that the sample initiation process is stable to within 30ns. Over a 10
second period, this means that we should be able to get 3 ppb accuracy in
frequency measurement. This corresponds to less than 10 mHz error for a 30MHz
signal. This method is limited on the Raspberry Pico to frequencies less than
roughly 50MHz, but there is a trick in frequency control with the Si5351 that
will let us apply this accuracy in measurement to get similar generation
accuracy in the 2m (144-148MHz), 1.25m (220-222MHz) and 70cm (420-450MHz) bands.

# Turning frequency measurements into frequency control

Once we get an accurate and high-resolution measurement, the question becomes
one of whether we can control a frequency generator precisely enough to get
precisely the output we want.

The system described here uses an Si5351 clock generator to generate the
transmitted signal. This chip generates a (nearly) square-wave signal on each of
three outputs. These outputs can be driven from either of two internal
phase-locked-loops (PLL) via a integer + fractional divider that has 20 bit
resolution. The PLLs can be configured to run in the range from 600-900MHz
referenced to an integrated crystal reference that runs in this design at 25MHz
via a similar integer + fractional divider.

Unfortunately, using a fixed 20bit denominator in the dividers in the Si5351
corresponds to about `n` ppm frequency resolution where `n` is the integer part
of the divider. For the 10m band `n` is no more than about 30 which means our
frequency control is 100-1000x coarser than our ability to measure our output
frequency. This may seem like splitting hairs, but the channel separation
between the FSK tones for WSPR is less than 1.5Hz and the specification requires
control over tone frequency to a fraction of the channel separation for maximum
noise resistance.

