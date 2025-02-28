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

package machine_x

import (
	"device/rp"
	"errors"
	"machine"
	"math"
	"runtime/volatile"
	"unsafe"
)

var (
	ErrBadPeriod = errors.New("period outside valid range 8ns..268ms")
)

const (
	maxPWMPins = 29
)

// pwmGroup is one PWM peripheral, which consists of a counter and two output
// channels. You can set the frequency using SetPeriod, but only for all the
// channels in this PWM peripheral at once.
//
// div: integer value to reduce counting rate by. Must be greater than or equal to 1.
//
// cc: counter compare level. Contains 2 channel levels. The LSBs are Channel A's level (Duty Cycle)
// and the MSBs are Channel B's level.
//
// top: Wrap. Highest number counter will reach before wrapping over. usually 0xffff.
//
// csr: Clock mode. PWM_CH0_CSR_DIVMODE_xxx registers have 4 possible modes, of which Free-running is used.
// csr contains output polarity bit at PWM_CH0_CSR_x_INV where x is the channel.
// csr contains phase correction bit at PWM_CH0_CSR_PH_CORRECT_Msk.
// csr contains PWM enable bit at PWM_CH0_CSR_EN. If not enabled PWM will not be active.
//
// ctr: PWM counter value.
type pwmGroup struct {
	CSR volatile.Register32
	DIV volatile.Register32
	CTR volatile.Register32
	CC  volatile.Register32
	TOP volatile.Register32
}

// Equivalent of
//
//	var pwmSlice []pwmGroup = (*[8]pwmGroup)(unsafe.Pointer(rp.PWM))[:]
//	return &pwmSlice[index]
//
// 0x14 is the size of a pwmGroup.
func getPWMGroup(index uintptr) *pwmGroup {
	return (*pwmGroup)(unsafe.Add(unsafe.Pointer(rp.PWM), 0x14*index))
}

// Hardware Pulse Width Modulation (PWM) API PWM peripherals available on RP2350.
// Each peripheral has 2 pins available for a total of 24 available PWM outputs.
// Some pins may not be available on some boards, but even if the pins aren't
// accessible, the PWM can be used for timing purposes.
//
// The RP2350 PWM block has 12 identical slices. Each slice can drive two PWM
// output signals, or measure the frequency or duty cycle of an input signal.
// This gives a total of up to 24 controllable PWM outputs. All 30 GPIOs can be
// driven by the PWM block
//
// The PWM hardware functions in output mode by continuously comparing the input
// value to a free-running counter. This produces a toggling output where the
// amount of time spent at the high output level is proportional to the input
// value. The fraction of time spent at the high signal level is known as the
// duty cycle of the signal. For input, the B pin becomes the clock for the PWM.
//
// The default behaviour of a PWM slice is to count upward until the wrap value
// (\ref pwm_config_set_wrap) is reached, and then immediately wrap to 0. PWM
// slices also offer a phase-correct mode, where the counter starts to count
// downward after reaching TOP, until it reaches 0 again.
var (
	PWM0  = getPWMGroup(0)
	PWM1  = getPWMGroup(1)
	PWM2  = getPWMGroup(2)
	PWM3  = getPWMGroup(3)
	PWM4  = getPWMGroup(4)
	PWM5  = getPWMGroup(5)
	PWM6  = getPWMGroup(6)
	PWM7  = getPWMGroup(7)
	PWM8  = getPWMGroup(7)
	PWM9  = getPWMGroup(7)
	PWM10 = getPWMGroup(7)
	PWM11 = getPWMGroup(7)
)

const (
	// these can be OR-ed together when calling SetEN_CH
	PWM_CH0 = 1 << iota
	PWM_CH1
	PWM_CH2
	PWM_CH3
	PWM_CH4
	PWM_CH5
	PWM_CH6
	PWM_CH7
	PWM_CH8
	PWM_CH9
	PWM_CH10
	PWM_CH11
)

// Configure enables and configures this PWM.
func (p *pwmGroup) Configure(config machine.PWMConfig) error {
	return p.init(config, true)
}

// Channel returns a PWM channel for the given pin. If pin does
// not belong to PWM peripheral ErrInvalidOutputPin error is returned.
// It also configures pin as PWM output.
func (p *pwmGroup) Channel(pin machine.Pin) (channel uint8, err error) {
	if pin > maxPWMPins || pwmGPIOToSlice(pin) != p.peripheral() {
		return 3, machine.ErrInvalidOutputPin
	}
	pin.Configure(machine.PinConfig{machine.PinPWM})
	return pwmGPIOToChannel(pin), nil
}

// Peripheral returns the RP2350 PWM peripheral which ranges from 0 to 11. Each
// PWM peripheral has 2 channels, A and B which correspond to 0 and 1 in the program.
// This number corresponds to the package's PWM0 through PWM11 handles
func PWMPeripheral(pin machine.Pin) (sliceNum uint8, err error) {
	if pin > maxPWMPins {
		return 0, machine.ErrInvalidOutputPin
	}
	return pwmGPIOToSlice(pin), nil
}

// returns the number of the pwm peripheral (0-11)
func (p *pwmGroup) peripheral() uint8 {
	return uint8((uintptr(unsafe.Pointer(p)) - uintptr(unsafe.Pointer(rp.PWM))) / 0x14)
}

// SetDivMode sets the mode for the PWM divider. The options are:
// rp.PWM_CH0_CSR_DIVMODE_DIV Free running
// rp.PWM_CH0_CSR_DIVMODE_FALL Increment on falling edge of B input
// rp.PWM_CH0_CSR_DIVMODE_LEVEL Increment when B is high
// rp.PWM_CH0_CSR_DIVMODE_RISE Increment on rising edge of B input
func (pwm *pwmGroup) SetDivMode(mode uint32) {
	pwm.CSR.ReplaceBits(mode, 3, rp.PWM_CH0_CSR_DIVMODE_Pos)
}

// SetClockDiv sets the rational division factor for the pwm clock using 8+4
func (pwm *pwmGroup) SetClockDiv(integerPart, frac uint32) {
	integerPart &= rp.PWM_CH0_DIV_INT_Msk >> rp.PWM_CH0_DIV_INT_Pos
	frac &= rp.PWM_CH0_DIV_FRAC_Msk >> rp.PWM_CH0_DIV_FRAC_Pos
	div := (integerPart << 4) + frac
	pwm.DIV.ReplaceBits(div, rp.PWM_CH0_DIV_FRAC_Msk|rp.PWM_CH0_DIV_INT_Msk, rp.PWM_CH0_DIV_FRAC_Pos)
}

// SetPeriod updates the period of this PWM peripheral in nanoseconds.
// To set a particular frequency, use the following formula:
//
//	period = 1e9 / frequency
//
// Where frequency is in hertz. If you use a period of 0, a period
// that works well for LEDs will be picked.
//
// SetPeriod will try not to modify TOP if possible to reach the target period.
// If the period is unattainable with current TOP SetPeriod will modify TOP
// by the bare minimum to reach the target period. It will also enable phase
// correct to reach periods above 130ms.
func (p *pwmGroup) SetPeriod(period uint64) error {
	if period == 0 {
		period = 1e5
	}
	return p.setPeriod(period)
}

// Top returns the current counter top, for use in duty cycle calculation.
//
// The value returned here is hardware dependent. In general, it's best to treat
// it as an opaque value that can be divided by some number and passed to Set
// (see Set documentation for more information).
func (p *pwmGroup) Top() uint32 {
	return p.getWrap()
}

// Counter returns the current counter value of the timer in this PWM
// peripheral. It may be useful for debugging.
func (p *pwmGroup) Counter() uint32 {
	return (p.CTR.Get() & rp.PWM_CH0_CTR_CH0_CTR_Msk) >> rp.PWM_CH0_CTR_CH0_CTR_Pos
}

// Period returns the used PWM period in nanoseconds.
func (p *pwmGroup) Period() uint64 {
	periodPerCycle := cpuPeriod()
	top := p.getWrap()
	phc := p.getPhaseCorrect()
	Int, frac := p.getClockDiv()
	// Line below can overflow if operations done without care.
	return (16*uint64(Int) + uint64(frac)) * uint64((top+1)*(phc+1)*periodPerCycle) / 16 // cycles = (TOP+1) * (CSRPHCorrect + 1) * (DIV_INT + DIV_FRAC/16)
}

// SetInverting sets whether to invert the output of this channel.
// Without inverting, a 25% duty cycle would mean the output is high for 25% of
// the time and low for the rest. Inverting flips the output as if a NOT gate
// was placed at the output, meaning that the output would be 25% low and 75%
// high with a duty cycle of 25%.
func (p *pwmGroup) SetInverting(channel uint8, inverting bool) {
	channel &= 1
	p.setInverting(channel, inverting)
}

// Set updates the channel value. This is used to control the channel duty
// cycle, in other words the fraction of time the channel output is high (or low
// when inverted). For example, to set it to a 25% duty cycle, use:
//
//	pwm.Set(channel, pwm.Top() / 4)
//
// pwm.Set(channel, 0) will set the output to low and pwm.Set(channel,
// pwm.Top()) will set the output to high, assuming the output isn't inverted.
func (p *pwmGroup) Set(channel uint8, value uint32) {
	val := uint16(value)
	channel &= 1
	p.setChanLevel(channel, val)
}

// Get current level (last set by Set). Default value on initialization is 0.
func (p *pwmGroup) Get(channel uint8) (value uint32) {
	channel &= 1
	return uint32(p.getChanLevel(channel))
}

// SetTop sets TOP control register. Max value is 16bit (0xffff).
func (p *pwmGroup) SetTop(top uint32) {
	p.setWrap(uint16(top))
}

// SetCounter sets counter control register. Max value is 16bit (0xffff).
// Useful for synchronising two different PWM peripherals.
func (p *pwmGroup) SetCounter(ctr uint32) {
	p.CTR.Set(ctr)
}

// Enable enables or disables PWM peripheral channels.
func (p *pwmGroup) Enable(enable bool) {
	p.enable(enable)
}

// IsEnabled returns true if peripheral is enabled.
func (p *pwmGroup) IsEnabled() (enabled bool) {
	return (p.CSR.Get()&rp.PWM_CH0_CSR_EN_Msk)>>rp.PWM_CH0_CSR_EN_Pos != 0
}

// Initialise a PWM with settings from a configuration object.
// If start is true then PWM starts on initialization.
func (p *pwmGroup) init(config machine.PWMConfig, start bool) error {
	// Not enable Phase correction
	p.setPhaseCorrect(false)

	// Clock mode set by default to Free running
	p.setDivMode(rp.PWM_CH0_CSR_DIVMODE_DIV)

	// Set Output polarity (false/false)
	p.setInverting(0, false)
	p.setInverting(1, false)

	// Set wrap. The highest value the counter will reach before returning to zero, also known as TOP.
	p.setWrap(0xffff)
	// period is set after TOP (Wrap).
	err := p.SetPeriod(config.Period)
	if err != nil {
		return err
	}
	// period already set beforea
	// Reset counter and compare (p level set to zero)
	p.CTR.ReplaceBits(0, rp.PWM_CH0_CTR_CH0_CTR_Msk, 0) // PWM_CH0_CTR_RESET
	p.CC.Set(0)                                         // PWM_CH0_CC_RESET

	p.enable(start)
	return nil
}

func SetEN_CH(channels, value uint32) {
	mask := ^(channels & 0xfe)
	if value != 0 {
		value = channels
	} else {
		value = 0
	}
	old := rp.PWM.EN.Get()
	rp.PWM.EN.Set(old&mask | value)
}

func (p *pwmGroup) setPhaseCorrect(correct bool) {
	p.CSR.ReplaceBits(boolToBit(correct)<<rp.PWM_CH0_CSR_PH_CORRECT_Pos, rp.PWM_CH0_CSR_PH_CORRECT_Msk, 0)
}

// Takes any of the following:
//
//	rp.PWM_CH0_CSR_DIVMODE_DIV, rp.PWM_CH0_CSR_DIVMODE_FALL,
//	rp.PWM_CH0_CSR_DIVMODE_LEVEL, rp.PWM_CH0_CSR_DIVMODE_RISE
func (p *pwmGroup) setDivMode(mode uint32) {
	p.CSR.ReplaceBits(mode<<rp.PWM_CH0_CSR_DIVMODE_Pos, rp.PWM_CH0_CSR_DIVMODE_Msk, 0)
}

// setPeriod sets the pwm peripheral period (frequency). Calculates DIV_INT,DIV_FRAC and sets it from following equation:
//
//	cycles = (TOP+1) * (CSRPHCorrect + 1) * (DIV_INT + DIV_FRAC/16)
//
// where cycles is amount of clock cycles per PWM period.
func (p *pwmGroup) setPeriod(period uint64) error {
	// This period calculation algorithm consists of
	// 1. Calculating best-fit prescale at a slightly lower-than-max TOP value
	// 2. Calculate TOP value to reach target period given the calculated prescale
	// 3. Apply calculated Prescale from step 1 and calculated Top from step 2
	const (
		maxTop = math.MaxUint16
		// start algorithm at 95% Top. This allows us to undershoot period with prescale.
		topStart     = 95 * maxTop / 100
		nanosecond   = 1                  // 1e-9 [s]
		microsecond  = 1000 * nanosecond  // 1e-6 [s]
		milliseconds = 1000 * microsecond // 1e-3 [s]
		// Maximum Period is 268369920ns on rp2040, given by (16*255+15)*8*(1+0xffff)*(1+1)/16
		// With no phase shift max period is half of this value.
		maxPeriod = 268 * milliseconds
	)

	if period > maxPeriod || period < 8 {
		return ErrBadPeriod
	}
	if period > maxPeriod/2 {
		p.setPhaseCorrect(true) // Must enable Phase correct to reach large periods.
	}

	// clearing above expression:
	//  DIV_INT + DIV_FRAC/16 = cycles / ( (TOP+1) * (CSRPHCorrect+1) )  // DIV_FRAC/16 is always 0 in this equation
	// where cycles must be converted to time:
	//  target_period = cycles * period_per_cycle ==> cycles = target_period/period_per_cycle
	periodPerCycle := uint64(cpuPeriod())
	phc := uint64(p.getPhaseCorrect())
	rhs := 16 * period / ((1 + phc) * periodPerCycle * (1 + topStart)) // right-hand-side of equation, scaled so frac is not divided
	whole := rhs / 16
	frac := rhs % 16
	switch {
	case whole > 0xff:
		whole = 0xff
	case whole == 0:
		// whole calculation underflowed so setting to minimum
		// permissible value in DIV_INT register.
		whole = 1
		frac = 0
	}

	// Step 2 is acquiring a better top value. Clearing the equation:
	// TOP =  cycles / ( (DIVINT+DIVFRAC/16) * (CSRPHCorrect+1) ) - 1
	top := 16*period/((16*whole+frac)*periodPerCycle*(1+phc)) - 1
	if top > maxTop {
		top = maxTop
	}
	p.SetTop(uint32(top))
	p.setClockDiv(uint8(whole), uint8(frac))
	return nil
}

// Int is integer value to reduce counting rate by. Must be greater than or equal to 1. DIV_INT is bits 4:11 (8 bits).
// frac's (DIV_FRAC) default value on reset is 0. Max value for frac is 15 (4 bits). This is known as a fixed-point
// fractional number.
//
//	cycles = (TOP+1) * (CSRPHCorrect + 1) * (DIV_INT + DIV_FRAC/16)
func (p *pwmGroup) setClockDiv(Int, frac uint8) {
	p.DIV.ReplaceBits((uint32(frac)<<rp.PWM_CH0_DIV_FRAC_Pos)|
		u32max(uint32(Int), 1)<<rp.PWM_CH0_DIV_INT_Pos, rp.PWM_CH0_DIV_FRAC_Msk|rp.PWM_CH0_DIV_INT_Msk, 0)
}

// Set the highest value the counter will reach before returning to 0. Also
// known as TOP.
//
// The counter wrap value is double-buffered in hardware. This means that,
// when the PWM is running, a write to the counter wrap value does not take
// effect until after the next time the PWM slice wraps (or, in phase-correct
// mode, the next time the slice reaches 0). If the PWM is not running, the
// write is latched in immediately.
func (p *pwmGroup) setWrap(wrap uint16) {
	p.TOP.ReplaceBits(uint32(wrap)<<rp.PWM_CH0_TOP_CH0_TOP_Pos, rp.PWM_CH0_TOP_CH0_TOP_Msk, 0)
}

// enables/disables the PWM peripheral with rp.PWM_CH0_CSR_EN bit.
func (p *pwmGroup) enable(enable bool) {
	p.CSR.ReplaceBits(boolToBit(enable)<<rp.PWM_CH0_CSR_EN_Pos, rp.PWM_CH0_CSR_EN_Msk, 0)
}

func (p *pwmGroup) setInverting(channel uint8, invert bool) {
	var pos uint8
	var msk uint32
	switch channel {
	case 0:
		pos = rp.PWM_CH0_CSR_A_INV_Pos
		msk = rp.PWM_CH0_CSR_A_INV_Msk
	case 1:
		pos = rp.PWM_CH0_CSR_B_INV_Pos
		msk = rp.PWM_CH0_CSR_B_INV_Msk
	}
	p.CSR.ReplaceBits(boolToBit(invert)<<pos, msk, 0)
}

// Set the current PWM counter compare value for one channel
//
// The counter compare register is double-buffered in hardware. This means
// that, when the PWM is running, a write to the counter compare values does
// not take effect until the next time the PWM slice wraps (or, in
// phase-correct mode, the next time the slice reaches 0). If the PWM is not
// running, the write is latched in immediately.
// Channel is 0 for A, 1 for B.
func (p *pwmGroup) setChanLevel(channel uint8, level uint16) {
	var pos uint8
	var mask uint32
	switch channel {
	case 0:
		pos = rp.PWM_CH0_CC_A_Pos
		mask = rp.PWM_CH0_CC_A_Msk
	case 1:
		pos = rp.PWM_CH0_CC_B_Pos
		mask = rp.PWM_CH0_CC_B_Msk
	}
	p.CC.ReplaceBits(uint32(level)<<pos, mask, 0)
}

func (p *pwmGroup) getChanLevel(channel uint8) (level uint16) {
	var pos uint8
	var mask uint32
	switch channel {
	case 0:
		pos = rp.PWM_CH0_CC_A_Pos
		mask = rp.PWM_CH0_CC_A_Msk
	case 1:
		pos = rp.PWM_CH0_CC_B_Pos
		mask = rp.PWM_CH0_CC_B_Msk
	}

	level = uint16((p.CC.Get() & mask) >> pos)
	return level
}

func (p *pwmGroup) getWrap() (top uint32) {
	return (p.TOP.Get() & rp.PWM_CH0_TOP_CH0_TOP_Msk) >> rp.PWM_CH0_TOP_CH0_TOP_Pos
}

func (p *pwmGroup) getPhaseCorrect() (phCorrect uint32) {
	return (p.CSR.Get() & rp.PWM_CH0_CSR_PH_CORRECT_Msk) >> rp.PWM_CH0_CSR_PH_CORRECT_Pos
}

func (p *pwmGroup) getClockDiv() (Int, frac uint8) {
	div := p.DIV.Get()
	return uint8((div & rp.PWM_CH0_DIV_INT_Msk) >> rp.PWM_CH0_DIV_INT_Pos), uint8((div & rp.PWM_CH0_DIV_FRAC_Msk) >> rp.PWM_CH0_DIV_FRAC_Pos)
}

// pwmGPIOToSlice Determine the PWM channel that is attached to the specified GPIO.
// gpio must be less than 30. Returns the PWM slice number that controls the specified GPIO.
func pwmGPIOToSlice(gpio machine.Pin) (slicenum uint8) {
	return (uint8(gpio) >> 1) & 7
}

// Determine the PWM channel that is attached to the specified GPIO.
// Each slice 0 to 7 has two channels, A and B.
func pwmGPIOToChannel(gpio machine.Pin) (channel uint8) {
	return uint8(gpio) & 1
}

// Returns the period of a clock cycle for the raspberry pi pico in nanoseconds.
// Used in PWM API.
func cpuPeriod() uint32 {
	return 1e9 / machine.CPUFrequency()
}
