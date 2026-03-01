// SPDX-License-Identifier: Apache-2.0

package firmware

import (
	"github.com/BitBoxSwiss/bitbox02-api-go/api/firmware/messages"
	"github.com/BitBoxSwiss/bitbox02-api-go/util/errp"
	"google.golang.org/protobuf/encoding/protowire"
)

// querySOL sends a Solana request over the generic request/response envelope using unknown fields.
func (device *Device) querySOL(solanaRequest []byte) ([]byte, error) {
	request := &messages.Request{}
	request.ProtoReflect().SetUnknown(protowire.AppendTag(nil, 31, protowire.BytesType))
	unknown := request.ProtoReflect().GetUnknown()
	unknown = append(unknown, protowire.AppendBytes(nil, solanaRequest)...)
	request.ProtoReflect().SetUnknown(unknown)

	response, err := device.query(request)
	if err != nil {
		return nil, err
	}
	unknownResp := response.ProtoReflect().GetUnknown()
	for len(unknownResp) > 0 {
		num, typ, n := protowire.ConsumeTag(unknownResp)
		if n < 0 {
			return nil, errp.New("invalid solana response tag")
		}
		unknownResp = unknownResp[n:]
		if typ != protowire.BytesType {
			m := protowire.ConsumeFieldValue(num, typ, unknownResp)
			if m < 0 {
				return nil, errp.New("invalid solana response field")
			}
			unknownResp = unknownResp[m:]
			continue
		}
		value, m := protowire.ConsumeBytes(unknownResp)
		if m < 0 {
			return nil, errp.New("invalid solana response bytes")
		}
		unknownResp = unknownResp[m:]
		if num == 18 {
			return value, nil
		}
	}
	return nil, errp.New("missing solana response")
}

func solEncodePubRequest(keypath []uint32, display bool) []byte {
	pubRequest := []byte{}
	if len(keypath) > 0 {
		packed := []byte{}
		for _, step := range keypath {
			packed = protowire.AppendVarint(packed, uint64(step))
		}
		pubRequest = protowire.AppendTag(pubRequest, 1, protowire.BytesType)
		pubRequest = protowire.AppendBytes(pubRequest, packed)
	}
	pubRequest = protowire.AppendTag(pubRequest, 2, protowire.VarintType)
	if display {
		pubRequest = protowire.AppendVarint(pubRequest, 1)
	} else {
		pubRequest = protowire.AppendVarint(pubRequest, 0)
	}

	solanaRequest := protowire.AppendTag(nil, 1, protowire.BytesType)
	solanaRequest = protowire.AppendBytes(solanaRequest, pubRequest)
	return solanaRequest
}

func solEncodeSignRequest(keypath []uint32, message []byte) []byte {
	signRequest := []byte{}
	if len(keypath) > 0 {
		packed := []byte{}
		for _, step := range keypath {
			packed = protowire.AppendVarint(packed, uint64(step))
		}
		signRequest = protowire.AppendTag(signRequest, 1, protowire.BytesType)
		signRequest = protowire.AppendBytes(signRequest, packed)
	}
	signRequest = protowire.AppendTag(signRequest, 2, protowire.BytesType)
	signRequest = protowire.AppendBytes(signRequest, message)

	solanaRequest := protowire.AppendTag(nil, 2, protowire.BytesType)
	solanaRequest = protowire.AppendBytes(solanaRequest, signRequest)
	return solanaRequest
}

func solDecodePubResponse(payload []byte) (string, error) {
	for len(payload) > 0 {
		num, typ, n := protowire.ConsumeTag(payload)
		if n < 0 {
			return "", errp.New("invalid solana response")
		}
		payload = payload[n:]
		if num == 1 && typ == protowire.BytesType {
			pubResponse, m := protowire.ConsumeBytes(payload)
			if m < 0 {
				return "", errp.New("invalid solana pub response")
			}
			for len(pubResponse) > 0 {
				n2, t2, x := protowire.ConsumeTag(pubResponse)
				if x < 0 {
					return "", errp.New("invalid solana pub response tag")
				}
				pubResponse = pubResponse[x:]
				if n2 == 1 && t2 == protowire.BytesType {
					pub, y := protowire.ConsumeString(pubResponse)
					if y < 0 {
						return "", errp.New("invalid solana pub response data")
					}
					return pub, nil
				}
				y := protowire.ConsumeFieldValue(n2, t2, pubResponse)
				if y < 0 {
					return "", errp.New("invalid solana pub response value")
				}
				pubResponse = pubResponse[y:]
			}
			payload = payload[m:]
			continue
		}
		m := protowire.ConsumeFieldValue(num, typ, payload)
		if m < 0 {
			return "", errp.New("invalid solana response field")
		}
		payload = payload[m:]
	}
	return "", errp.New("missing solana pub response")
}

func solDecodeSignResponse(payload []byte) ([]byte, []byte, error) {
	for len(payload) > 0 {
		num, typ, n := protowire.ConsumeTag(payload)
		if n < 0 {
			return nil, nil, errp.New("invalid solana response")
		}
		payload = payload[n:]
		if num == 2 && typ == protowire.BytesType {
			signResponse, m := protowire.ConsumeBytes(payload)
			if m < 0 {
				return nil, nil, errp.New("invalid solana sign response")
			}
			var signature []byte
			var publicKey []byte
			for len(signResponse) > 0 {
				n2, t2, x := protowire.ConsumeTag(signResponse)
				if x < 0 {
					return nil, nil, errp.New("invalid solana sign response tag")
				}
				signResponse = signResponse[x:]
				switch {
				case n2 == 1 && t2 == protowire.BytesType:
					v, y := protowire.ConsumeBytes(signResponse)
					if y < 0 {
						return nil, nil, errp.New("invalid solana signature")
					}
					signature = append([]byte{}, v...)
					signResponse = signResponse[y:]
				case n2 == 2 && t2 == protowire.BytesType:
					v, y := protowire.ConsumeBytes(signResponse)
					if y < 0 {
						return nil, nil, errp.New("invalid solana public key")
					}
					publicKey = append([]byte{}, v...)
					signResponse = signResponse[y:]
				default:
					y := protowire.ConsumeFieldValue(n2, t2, signResponse)
					if y < 0 {
						return nil, nil, errp.New("invalid solana sign response value")
					}
					signResponse = signResponse[y:]
				}
			}
			if len(signature) == 0 {
				return nil, nil, errp.New("missing solana signature")
			}
			return signature, publicKey, nil
		}
		m := protowire.ConsumeFieldValue(num, typ, payload)
		if m < 0 {
			return nil, nil, errp.New("invalid solana response field")
		}
		payload = payload[m:]
	}
	return nil, nil, errp.New("missing solana sign response")
}

// SolanaPub queries a Solana address at the given keypath.
func (device *Device) SolanaPub(keypath []uint32, display bool) (string, error) {
	rawResponse, err := device.querySOL(solEncodePubRequest(keypath, display))
	if err != nil {
		return "", err
	}
	return solDecodePubResponse(rawResponse)
}

// SolanaSignTransaction signs a serialized Solana transaction message.
func (device *Device) SolanaSignTransaction(keypath []uint32, message []byte) ([]byte, []byte, error) {
	rawResponse, err := device.querySOL(solEncodeSignRequest(keypath, message))
	if err != nil {
		return nil, nil, err
	}
	return solDecodeSignResponse(rawResponse)
}
