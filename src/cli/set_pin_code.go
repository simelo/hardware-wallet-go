package cli

import (
	"fmt"

	gcli "github.com/urfave/cli"

	deviceWallet "github.com/skycoin/hardware-wallet-go/src/device-wallet"
	messages "github.com/skycoin/hardware-wallet-go/src/device-wallet/messages/go"
)

func setPinCode() gcli.Command {
	name := "setPinCode"
	return gcli.Command{
		Name:        name,
		Usage:       "Configure a PIN code on a device.",
		Description: "",
		Flags: []gcli.Flag{
			gcli.StringFlag{
				Name:   "deviceType",
				Usage:  "Device type to send instructions to, hardware wallet (USB) or emulator.",
				EnvVar: "DEVICE_TYPE",
			},
		},
		OnUsageError: onCommandUsageError(name),
		Action: func(c *gcli.Context) {
			var device *deviceWallet.Device
			switch c.String("deviceType") {
			case "USB":
				device = deviceWallet.NewUSBDevice()
			case "EMULATOR":
				device = deviceWallet.NewEmulatorDevice()
			default:
				log.Error("device type not set")
				return
			}

			var pinEnc string
			msg, err := device.ChangePin()
			if err != nil {
				log.Error(err)
				return
			}

			// TODO: can PinMatrixAck return MessageType_MessageType_PinMatrixRequest? figure out
			for msg.Kind == uint16(messages.MessageType_MessageType_PinMatrixRequest) {
				fmt.Printf("PinMatrixRequest response: ")
				fmt.Scanln(&pinEnc)
				msg, err = device.PinMatrixAck(pinEnc)
				if err != nil {
					log.Error(err)
					return
				}
			}
		},
	}
}
