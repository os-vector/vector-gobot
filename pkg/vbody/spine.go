package vbody

// #cgo LDFLAGS: -L${SRCDIR}/../.. -lvector-gobot -ldl
// #cgo CFLAGS: -I${SRCDIR}/../../include -w
// #include "libvector_gobot.h"
// #include "spine.h"
import "C"
import (
	"errors"
	"fmt"
	"time"
)

var ReadOnly bool = false

const (
	LED_GREEN = 0x00FF00
	LED_RED   = 0x0000FF
	LED_BLUE  = 0xFF0000
	LED_OFF   = 0x0
)

type MotorStatus [4]struct {
	Pos int32
	DLT int32
	TM  uint32
}

// This is the data each spine frame from GetFrame() contains.
type DataFrame struct {
	Seq    uint32
	Cliffs [4]uint32
	// right wheel, left wheel, lift, head
	Encoders           MotorStatus
	BattVoltage        int16
	ChargerVoltage     int16
	BodyTemp           int16
	Touch              uint16
	ButtonState        bool
	MicData            []int16
	ProxSigmaMM        uint8
	ProxRawRangeMM     uint16
	ProxSignalRateMCPS uint16
	ProxAmbient        uint16
	ProxSPADCount      uint16
	ProxSampleCount    uint16
	ProxCalibResult    uint32
}

var spineHandle int
var spineInited bool
var motor1 int16
var motor2 int16
var motor3 int16
var motor4 int16
var frontLEDStatus uint32 = LED_OFF
var middleLEDStatus uint32 = LED_OFF
var backLEDStatus uint32 = LED_OFF

var frameChannel chan DataFrame
var buttonChannel chan bool

/*
Check if body comms are initiated.
*/
func IsInited() bool {
	return spineInited
}

/*
Init spine communication. This must be run before you try to get a frame.
Starts getting and sending frames, filling the currentDataFrame buffer.
Checks if body is functional.
*/
func InitSpine() error {
	if spineInited {
		fmt.Println("Spine already initiated, handle " + fmt.Sprint(spineHandle))
		return nil
	}
	handle := C.spine_full_init()
	if handle > 0 {
		spineInited = true
		err := startCommsLoop()
		if err != nil {
			fmt.Println("error initializing spine. is vic-robot still alive?")
			spineInited = false
			return err
		}
	} else {
		spineInited = false
		fmt.Println("error initializing spine. is vic-robot still alive?")
		return errors.New("spine handle is 0")
	}
	return nil
}

/*
Close communication channel with body, stop sending/getting frames.
*/
func StopSpine() {
	if spineInited {
		spineInited = false
		time.Sleep(time.Millisecond * 50)
		C.close_spine()
	}
}

/*
Use LED_GREEN, LED_BLUE, and LED_RED consts as reference.
*/
func SetLEDs(front uint32, middle uint32, back uint32) error {
	if !spineInited {
		return errors.New("initiate spine first")
	}
	frontLEDStatus, middleLEDStatus, backLEDStatus = front, middle, back
	return nil
}

// rwheel, lwheel, lift, head
func startCommsLoop() error {
	if !spineInited {
		return errors.New("initiate spine first")
	}

	// check if body is responding
	// read 10 frames, make sure touch sensor is providing valid data
	for i := 0; i <= 10; i++ {
		if !spineInited {
			return errors.New("spine became uninitialized during comms loop start")
		}
		frame := readFrame()
		if i == 10 && frame.Touch == 0 {
			return errors.New("body hasn't returned a valid frame after " + fmt.Sprint(i) + " tries")
		} else if frame.Touch == 0 {
			time.Sleep(time.Millisecond * 10)
			continue
		} else {
			break
		}
	}

	frameChannel = make(chan DataFrame, 1)
	var frameChannelForButton = make(chan DataFrame, 1)

	if !ReadOnly {
		go func() {
			ticker := time.NewTicker(time.Millisecond * 10)
			defer ticker.Stop()
			seq := 8888
			for range ticker.C {
				if !spineInited {
					return
				}
				var motors []int16 = []int16{motor1, motor2, motor3, motor4}
				var leds []uint32 = []uint32{backLEDStatus, middleLEDStatus, frontLEDStatus, frontLEDStatus}
				C.spine_full_update(C.uint32_t(seq), (*C.int16_t)(&motors[0]), (*C.uint32_t)(&leds[0]))
			}
		}()
	}

	var returnFrame bool

	// we HAVE to keep the tty buffer from stalling, which means we have to get frames very quickly :/
	go func() {
		for {
			// only return half the frames

			if !spineInited {
				return
			}

			frame := readFrame()
			if !returnFrame {
				returnFrame = true
				continue
			}
			returnFrame = false

			select {
			case frameChannel <- frame:
			default:
			}
			select {
			case frameChannelForButton <- frame:
			default:
			}
		}
	}()

	buttonChannel = make(chan bool, 1)

	go func() {
		var bStatus bool
		for fr := range frameChannelForButton {
			if !spineInited {
				return
			}
			if !bStatus && fr.ButtonState {
				bStatus = true
				select {
				case buttonChannel <- true:
				default:
				}
			} else if bStatus && !fr.ButtonState {
				bStatus = false
				select {
				case buttonChannel <- false:
				default:
				}
			}
		}
	}()

	time.Sleep(time.Second)
	return nil
}

/*
Get a frame channel. In your code, you should use it like:

frameChan := GetFrameChan()

	for frame := range frameChan {
		<do whatever you need to with frame>
		<this will repeat whenever a new frame is ready>
	}
*/
func GetFrameChan() chan DataFrame {
	return frameChannel
}

/*
Set the motors to desired speed. Accepts negative values.
Order (relative to robot): right wheel, left wheel, lift, head
*/
func SetMotors(m1 int16, m2 int16, m3 int16, m4 int16) error {
	m2 = -(m2)
	if !spineInited {
		return errors.New("initiate spine first")
	}
	motor1, motor2, motor3, motor4 = m1*100, m2*100, m3*100, m4*100
	return nil
}

/*
Get a button press channel.
Sends true when press, false when released.

frameChan := GetButtonChannel()

	for frame := range frameChan {
		<do whatever you need to with frame>
		<this will repeat whenever a new frame is ready>
	}
*/
func GetButtonChan() chan bool {
	return buttonChannel
}

func readFrame() DataFrame {
	returnFrame := DataFrame{}
	if !spineInited {
		return returnFrame
	}
	df := C.iterate()
	returnFrame.Seq = uint32(df.seq)
	goms := MotorStatus{}
	ms := df.motors
	for i := range ms {
		goms[i].Pos = int32(ms[i].pos)
		goms[i].DLT = int32(ms[i].dlt)
		goms[i].TM = uint32(ms[i].tm)
	}
	returnFrame.Encoders = goms
	returnFrame.Cliffs = [4]uint32{uint32(df.cliff_sensor[0]), uint32(df.cliff_sensor[1]), uint32(df.cliff_sensor[2]), uint32(df.cliff_sensor[3])}
	returnFrame.BattVoltage = int16(df.battery_voltage)
	returnFrame.ChargerVoltage = int16(df.charger_voltage)
	returnFrame.BodyTemp = int16(df.body_temp)
	returnFrame.ProxRawRangeMM = uint16(df.prox_raw_range_mm)
	returnFrame.Touch = uint16(df.touch_sensor)
	/*
			    uint8_t prox_sigma_mm;
		    uint16_t prox_raw_range_mm;
		    uint16_t prox_signal_rate_mcps;
		    uint16_t prox_ambient;
		    uint16_t prox_SPAD_count;
		    uint16_t prox_sample_count;
		    uint32_t prox_calibration_result;
	*/
	returnFrame.ProxSigmaMM = uint8(df.prox_sigma_mm)
	returnFrame.ProxRawRangeMM = uint16(df.prox_raw_range_mm)
	returnFrame.ProxSignalRateMCPS = uint16(df.prox_signal_rate_mcps)
	returnFrame.ProxAmbient = uint16(df.prox_ambient)
	returnFrame.ProxSPADCount = uint16(df.prox_SPAD_count)
	returnFrame.ProxSampleCount = uint16(df.prox_sample_count)
	returnFrame.ProxCalibResult = uint32(df.prox_calibration_result)
	//returnFrame.ProxRealMM = uint16(float64(returnFrame.ProxRawRangeMM) * (float64(returnFrame.ProxSignalRateMCPS) / float64(returnFrame.ProxAmbient)))
	if df.buttton_state > 0 {
		returnFrame.ButtonState = true
	}
	returnFrame.MicData = []int16{}
	for _, data := range df.mic_data {
		returnFrame.MicData = append(returnFrame.MicData, int16(data))
	}
	return returnFrame
}

/*
reading

typedef struct {
    uint32_t seq;
    uint16_t status;
    uint8_t i2c_device_fault;
    uint8_t i2c_fault_item;
    spine_motorstatus_t motors[4];
    uint16_t cliff_sensor[4];
    int16_t battery_voltage;
    int16_t charger_voltage;
    int16_t body_temp;
    uint16_t battery_flags;
    uint16_t __reserved1;
    uint8_t prox_sigma_mm;
    uint16_t prox_raw_range_mm;
    uint16_t prox_signal_rate_mcps;
    uint16_t prox_ambient;
    uint16_t prox_SPAD_count;
    uint16_t prox_sample_count;
    uint32_t prox_calibration_result;
    uint16_t touch_sensor;
    uint16_t buttton_state;
    uint32_t mic_indices;
    uint16_t button_inputs;
    uint8_t __reserved2[26];
    uint16_t mic_data[320];
} spine_dataframe_t;

typedef struct {
    int32_t pos;
    int32_t dlt;
    uint32_t tm;
} spine_motorstatus_t;
*/
