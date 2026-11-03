package market

import "time"

type OptionChain struct {
	Exchange   string         `json:"exchange"`
	Symbol     string         `json:"symbol"`
	Underlying float64        `json:"underlying"`
	Expiry     string         `json:"expiry"`
	ExpiresAt  time.Time      `json:"expiresAt"`
	UpdatedAt  time.Time      `json:"updatedAt"`
	PCR        float64        `json:"pcr"`
	PCRVolume  float64        `json:"pcrVolume"`
	CallOI     float64        `json:"callOI"`
	PutOI      float64        `json:"putOI"`
	CallVolume float64        `json:"callVolume"`
	PutVolume  float64        `json:"putVolume"`
	Support    float64        `json:"support"`
	Resistance float64        `json:"resistance"`
	MaxPain    float64        `json:"maxPain"`
	CallDelta  float64        `json:"callDelta"`
	PutDelta   float64        `json:"putDelta"`
	CallGamma  float64        `json:"callGamma"`
	PutGamma   float64        `json:"putGamma"`
	CallTheta  float64        `json:"callTheta"`
	PutTheta   float64        `json:"putTheta"`
	NetDelta   float64        `json:"netDelta"`
	NetGamma   float64        `json:"netGamma"`
	NetTheta   float64        `json:"netTheta"`
	Bias       string         `json:"bias"`
	Confidence float64        `json:"confidence"`
	Reason     string         `json:"reason"`
	Strikes    []OptionStrike `json:"strikes"`
}

type OptionStrike struct {
	Strike       float64 `json:"strike"`
	Expiry       string  `json:"expiry"`
	CallIV       float64 `json:"callIV"`
	PutIV        float64 `json:"putIV"`
	CallOI       float64 `json:"callOI"`
	PutOI        float64 `json:"putOI"`
	CallVolume   float64 `json:"callVolume"`
	PutVolume    float64 `json:"putVolume"`
	CallDelta    float64 `json:"callDelta"`
	PutDelta     float64 `json:"putDelta"`
	CallGamma    float64 `json:"callGamma"`
	PutGamma     float64 `json:"putGamma"`
	CallTheta    float64 `json:"callTheta"`
	PutTheta     float64 `json:"putTheta"`
	CallIVBidAsk float64 `json:"callLastPrice"`
	PutIVBidAsk  float64 `json:"putLastPrice"`
}
