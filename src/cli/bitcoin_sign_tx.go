package cli

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	messages "github.com/skycoin/hardware-wallet-protob/go"

	//"github.com/gogo/protobuf/proto"
	skyWallet "github.com/skycoin/hardware-wallet-go/src/skywallet"
	gcli "github.com/urfave/cli"
	"os"
)

func bitcoinSignTxCmd() gcli.Command {
	name := "btcSignTx"
	return gcli.Command{
		Name:  name,
		Usage: "Ask the device to sign a Bitcoin transaction using the provided information.",
		Flags: []gcli.Flag{
			gcli.StringFlag{
				Name:  "file",
				Usage: "Path to JSON file with all necessary information for signing process.",
			},
			gcli.StringFlag{
				Name:   "deviceType",
				Usage:  "Device type to send instructions to, hardware wallet (USB) or emulator.",
				EnvVar: "DEVICE_TYPE",
			},
		},
		OnUsageError: onCommandUsageError(name),
		Action: func(c *gcli.Context) {
			device := skyWallet.NewDevice(skyWallet.DeviceTypeFromString(c.String("deviceType")))
			if device == nil {
				return
			}
			defer device.Close()

			filePath := c.String("file")
			info, err := readFile(filePath)
			if err != nil {
				fmt.Println(err.Error())
				return
			}

			inputs, err := buildInputs(info.Inputs)
			if err != nil {
				println(err.Error())
				return
			}

			outputs, err := buildOutputs(info.Outputs)
			if err != nil {
				println(err.Error())
				return
			}

			inputsCnt := uint32(len(inputs))
			outputsCnt := uint32(len(outputs))

			txes := map[string]messages.BitcoinTxAck_TransactionType{}
			txes[""] = messages.BitcoinTxAck_TransactionType{
				Inputs:     inputs,
				Outputs:    outputs,
				InputsCnt:  &inputsCnt,
				OutputsCnt: &outputsCnt,
			}

			// Load prev txes
			for _, inp := range inputs {
				if *inp.ScriptType != messages.InputScriptType_SPENDP2SHWITNESS && *inp.ScriptType != messages.InputScriptType_SPENDWITNESS && *inp.ScriptType != messages.InputScriptType_EXTERNAL {
					prevHash := hex.EncodeToString(inp.PrevHash)
					if prevTx, ok := txes[prevHash]; ok {
						txes[prevHash] = prevTx
					} else {
						fmt.Printf("Could not retrieve prev_tx: %s\n", prevHash)
						return
					}
				}
			}
			signatures := make([][]byte, len(info.Inputs))
			var serializedTx []byte
			msg, err := device.BitcoinSignTx("Bitcoin", inputsCnt, outputsCnt)
			if err != nil {
				log.Error(err)
				return
			}
			for msg.Kind == uint16(messages.MessageType_MessageType_BitcoinTxRequest) {
				res, err := skyWallet.DecodeBitcoinTxRequestMessage(msg)
				if err != nil {
					println(err.Error())
				}
				if res.Serialized != nil {
					if res.Serialized.SerializedTx != nil {
						for i := 0; i < len(res.Serialized.SerializedTx); i++ {
							serializedTx = append(serializedTx, res.Serialized.SerializedTx[i])
						}
					}
					if res.Serialized.SignatureIndex != nil {
						idx := *res.Serialized.SignatureIndex
						sig := res.Serialized.Signature
						if signatures[idx] != nil {
							fmt.Printf("Signature for index %d already filled", idx)
							return
						}
						signatures[idx] = sig
					}
				}
				if *res.RequestType == messages.RequestType_TXFINISHED {
					break
				}
				current_tx := txes[hex.EncodeToString(res.Details.TxHash)]

				switch *res.RequestType {
				case messages.RequestType_TXMETA:
					tt := copyTransaction(current_tx)
					msg, err = device.BitcoinTxAck(&tt)
				case messages.RequestType_TXINPUT:
					tt := messages.BitcoinTxAck_TransactionType{}
					tt.Inputs = current_tx.Inputs[*res.Details.RequestIndex : *res.Details.RequestIndex+1]
					msg, err = device.BitcoinTxAck(&tt)
				case messages.RequestType_TXOUTPUT:
					tt := messages.BitcoinTxAck_TransactionType{}
					if res.Details.TxHash != nil {
						tt.BinOutputs = current_tx.BinOutputs[*res.Details.RequestIndex : *res.Details.RequestIndex+1]
					} else {
						tt.Outputs = current_tx.Outputs[*res.Details.RequestIndex : *res.Details.RequestIndex+1]
					}
					msg, err = device.BitcoinTxAck(&tt)
				case messages.RequestType_TXEXTRADATA:
					offset, l := *res.Details.ExtraDataOffset, *res.Details.ExtraDataLen
					tt := messages.BitcoinTxAck_TransactionType{}
					tt.ExtraData = current_tx.ExtraData[offset : offset+l]
					msg, err = device.BitcoinTxAck(&tt)
				}
			}

			if msg.Kind == uint16(messages.MessageType_MessageType_Failure) {
				println("Signing failed")
				return
			} else if msg.Kind != uint16(messages.MessageType_MessageType_TxRequest) {
				println("Unexpected message")
				return
			}
			for i := 0; i < len(signatures); i++ {
				if signatures[i] == nil {
					println("Some signatures are missing!")
					return
				}
			}
			fmt.Printf("Signed Transaction:\n%s\n", hex.EncodeToString(serializedTx))
		},
	}
}

func readFile(filePath string) (info SignInfo, err error) {
	file, err := os.Open(filePath)
	if err != nil {
		return
	}
	var data []byte
	buffer := [1]byte{}
	i, err := file.Read(buffer[:])
	for i > 0 {
		data = append(data, buffer[0])
		i, err = file.Read(buffer[:])
	}
	info = SignInfo{}
	err = json.Unmarshal(data, &info)
	return
}

type InputInfo struct {
	AddressN   []uint32
	PrevHash   string
	PrevIndex  uint32
	Script     string
	ScriptType string
	Amount     uint64
}

type OutputInfo struct {
	AddressN   []uint32
	Address    string
	ScriptType string
	Amount     uint64
}

type TransactionTypeInfo struct {
	Version      uint32
	Inputs       []InputInfo
	Outputs      []OutputInfo
	InputsCount  uint32
	OutputsCount uint32
	ExtraData    string
	ExtraDataLen uint32
}

type SignInfo struct {
	Inputs   []InputInfo
	Outputs  []OutputInfo
	PrevTxes []TransactionTypeInfo
}

func buildInputs(info []InputInfo) (inputs []*messages.TxInputType, err error) {
	if info == nil {
		return
	}
	inputs = make([]*messages.TxInputType, len(info))
	for i := 0; i < len(info); i++ {
		inputs[i] = &messages.TxInputType{}
		inputs[i].PrevHash, err = hex.DecodeString(info[i].PrevHash)
		if err != nil {
			return
		}
		inputs[i].Amount = &info[i].Amount
		inputs[i].AddressN = info[i].AddressN
		inputs[i].PrevIndex = &info[i].PrevIndex
		inputs[i].ScriptSig, err = hex.DecodeString(info[i].Script)
		if err != nil {
			return
		}
		scriptType := messages.InputScriptType_value[info[i].ScriptType]
		a := messages.InputScriptType(scriptType)
		inputs[i].ScriptType = &a
	}
	return
}

func buildOutputs(info []OutputInfo) (outputs []*messages.TxOutputType, err error) {
	if info == nil {
		return
	}
	outputs = make([]*messages.TxOutputType, len(info))
	for i := 0; i < len(info); i++ {
		outputs[i] = &messages.TxOutputType{}
		scriptType := messages.OutputScriptType_value[info[i].ScriptType]
		a := messages.OutputScriptType(scriptType)
		outputs[i].ScriptType = &a
		outputs[i].AddressN = info[i].AddressN
		outputs[i].Address = &info[i].Address
		outputs[i].Amount = &info[i].Amount
	}
	return
}

func buildPrevTxes(info []TransactionTypeInfo) (prevTxes []*messages.TransactionType, err error) {
	prevTxes = make([]*messages.TransactionType, len(info))
	for i := 0; i < len(prevTxes); i++ {
		prevTxes[i] = &messages.TransactionType{}
		prevTxes[i].ExtraData, err = hex.DecodeString(info[i].ExtraData)
		if err != nil {
			return
		}
		prevTxes[i].ExtraDataLen = &info[i].ExtraDataLen
		prevTxes[i].Outputs, err = buildOutputs(info[i].Outputs)
		if err != nil {
			return
		}
		prevTxes[i].OutputsCnt = &info[i].OutputsCount
		if len(prevTxes[i].Outputs) != int(*prevTxes[i].OutputsCnt) {
			fmt.Printf("Invlid outputs count\n")
			return
		}
		prevTxes[i].Inputs, err = buildInputs(info[i].Inputs)
		if err != nil {
			return
		}
		prevTxes[i].InputsCnt = &info[i].InputsCount
		if len(prevTxes[i].Inputs) != int(*prevTxes[i].InputsCnt) {
			fmt.Printf("Invalid inputs count\n")
			return
		}
	}
	return
}

func copyTransaction(tx messages.BitcoinTxAck_TransactionType) (copy messages.BitcoinTxAck_TransactionType) {
	copy = tx
	inputsCnt := uint32(len(tx.Inputs))
	copy.InputsCnt = &inputsCnt
	copy.Inputs = []*messages.TxInputType{}
	outputsCnt := uint32(len(tx.Outputs))
	copy.OutputsCnt = &outputsCnt
	copy.Outputs = []*messages.TxOutputType{}
	copy.BinOutputs = []*messages.TxOutputBinType{}
	var extraDataLen uint32 = 0
	if tx.ExtraData != nil {
		extraDataLen = uint32(len(tx.ExtraData))
	}
	copy.ExtraDataLen = &extraDataLen
	copy.ExtraData = nil
	return
}
