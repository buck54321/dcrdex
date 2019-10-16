package msgjson

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestMatch(t *testing.T) {
	// serialization: orderid (32) + matchid (8) + qty (8) + rate (8)
	// + address (varies)
	oid, _ := BytesFromHex("2219c5f3a03407c87211748c884404e2f466cba19616faca1cda0010ca5db0d3")
	mid, _ := BytesFromHex("4969784b00a59dd0340952c9b8f52840fbb32e9b51d4f6e18cbec7f50c8a3ed7")
	match := &Match{
		OrderID:  oid,
		MatchID:  mid,
		Quantity: 5e8,
		Rate:     uint64(2e8),
		Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
		Time:     1570668234,
	}
	exp := []byte{
		// Order ID 32 bytes
		0x22, 0x19, 0xc5, 0xf3, 0xa0, 0x34, 0x07, 0xc8, 0x72, 0x11, 0x74, 0x8c, 0x88,
		0x44, 0x04, 0xe2, 0xf4, 0x66, 0xcb, 0xa1, 0x96, 0x16, 0xfa, 0xca, 0x1c, 0xda,
		0x00, 0x10, 0xca, 0x5d, 0xb0, 0xd3,
		// Match ID 32 bytes
		0x49, 0x69, 0x78, 0x4b, 0x00, 0xa5, 0x9d, 0xd0, 0x34, 0x09, 0x52, 0xc9, 0xb8,
		0xf5, 0x28, 0x40, 0xfb, 0xb3, 0x2e, 0x9b, 0x51, 0xd4, 0xf6, 0xe1, 0x8c, 0xbe,
		0xc7, 0xf5, 0x0c, 0x8a, 0x3e, 0xd7,
		// quantity 8 bytes
		0x00, 0x00, 0x00, 0x00, 0x1d, 0xcd, 0x65, 0x00,
		// rate 8 bytes
		0x00, 0x00, 0x00, 0x00, 0x0b, 0xeb, 0xc2, 0x00,
		// timestamp
		0x00, 0x00, 0x00, 0x00, 0x5d, 0x9e, 0x7e, 0xca,
		// address - utf-8 encoding
		0x44, 0x63, 0x71, 0x58, 0x73, 0x77, 0x6a, 0x54, 0x50, 0x6e, 0x55, 0x63, 0x64,
		0x34, 0x46, 0x52, 0x43, 0x6b, 0x58, 0x34, 0x76, 0x52, 0x4a, 0x78, 0x6d, 0x56,
		0x74, 0x66, 0x67, 0x47, 0x56, 0x61, 0x35, 0x75, 0x69,
	}

	b, err := match.Serialize()
	if err != nil {
		t.Fatalf("serialization error: %v", err)
	}
	if !bytes.Equal(b, exp) {
		t.Fatalf("unexpected serialization. Wanted %x, got %x", exp, b)
	}

	matchB, err := json.Marshal(match)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var matchBack Match
	err = json.Unmarshal(matchB, &matchBack)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if !bytes.Equal(matchBack.MatchID, match.MatchID) {
		t.Fatal(matchBack.MatchID, match.MatchID)
	}
	if !bytes.Equal(matchBack.OrderID, match.OrderID) {
		t.Fatal(matchBack.OrderID, match.OrderID)
	}
	if matchBack.Quantity != match.Quantity {
		t.Fatal(matchBack.Quantity, match.Quantity)
	}
	if matchBack.Rate != match.Rate {
		t.Fatal(matchBack.Rate, match.Rate)
	}
	if matchBack.Address != match.Address {
		t.Fatal(matchBack.Address, match.Address)
	}
	if matchBack.Time != match.Time {
		t.Fatal(matchBack.Time, match.Time)
	}
}

func TestInit(t *testing.T) {
	// serialization: orderid (32) + matchid (32) + txid (probably 64) + vout (4)
	// + timestamp (8) + contract (97 ish)
	oid, _ := BytesFromHex("ceb09afa675cee31c0f858b94c81bd1a4c2af8c5947d13e544eef772381f2c8d")
	mid, _ := BytesFromHex("7c6b44735e303585d644c713fe0e95897e7e8ba2b9bba98d6d61b70006d3d58c")
	contract, _ := BytesFromHex("caf8d277f80f71e4")
	init := &Init{
		OrderID:  oid,
		MatchID:  mid,
		TxID:     "c3161033de096fd74d9051ff0bd99e359de35080a3511081ed035f541b850d43",
		Vout:     10,
		Time:     1570704776,
		Contract: contract,
	}

	exp := []byte{
		// Order ID 32 bytes
		0xce, 0xb0, 0x9a, 0xfa, 0x67, 0x5c, 0xee, 0x31, 0xc0, 0xf8, 0x58, 0xb9,
		0x4c, 0x81, 0xbd, 0x1a, 0x4c, 0x2a, 0xf8, 0xc5, 0x94, 0x7d, 0x13, 0xe5,
		0x44, 0xee, 0xf7, 0x72, 0x38, 0x1f, 0x2c, 0x8d,
		// Match ID 32 bytes
		0x7c, 0x6b, 0x44, 0x73, 0x5e, 0x30, 0x35, 0x85, 0xd6, 0x44, 0xc7, 0x13,
		0xfe, 0x0e, 0x95, 0x89, 0x7e, 0x7e, 0x8b, 0xa2, 0xb9, 0xbb, 0xa9, 0x8d,
		0x6d, 0x61, 0xb7, 0x00, 0x06, 0xd3, 0xd5, 0x8c,
		// Transaction ID 64 bytes utf-8
		0x63, 0x33, 0x31, 0x36, 0x31, 0x30, 0x33, 0x33, 0x64, 0x65, 0x30, 0x39,
		0x36, 0x66, 0x64, 0x37, 0x34, 0x64, 0x39, 0x30, 0x35, 0x31, 0x66, 0x66,
		0x30, 0x62, 0x64, 0x39, 0x39, 0x65, 0x33, 0x35, 0x39, 0x64, 0x65, 0x33,
		0x35, 0x30, 0x38, 0x30, 0x61, 0x33, 0x35, 0x31, 0x31, 0x30, 0x38, 0x31,
		0x65, 0x64, 0x30, 0x33, 0x35, 0x66, 0x35, 0x34, 0x31, 0x62, 0x38, 0x35,
		0x30, 0x64, 0x34, 0x33,
		// Vout 4 bytes
		0x00, 0x00, 0x00, 0x0a,
		// Timestamp 8 bytes
		0x00, 0x00, 0x00, 0x00, 0x5d, 0x9f, 0x0d, 0x88,
		// Contract 8 bytes (shortened for testing)
		0xca, 0xf8, 0xd2, 0x77, 0xf8, 0x0f, 0x71, 0xe4,
	}
	b, err := init.Serialize()
	if err != nil {
		t.Fatalf("serialization error: %v", err)
	}
	if !bytes.Equal(b, exp) {
		t.Fatalf("unexpected serialization. Wanted %x, got %x", exp, b)
	}

	initB, err := json.Marshal(init)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var initBack Init
	err = json.Unmarshal(initB, &initBack)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if !bytes.Equal(initBack.MatchID, init.MatchID) {
		t.Fatal(initBack.MatchID, init.MatchID)
	}
	if !bytes.Equal(initBack.OrderID, init.OrderID) {
		t.Fatal(initBack.OrderID, init.OrderID)
	}
	if initBack.TxID != init.TxID {
		t.Fatal(initBack.TxID, init.TxID)
	}
	if initBack.Vout != init.Vout {
		t.Fatal(initBack.Vout, init.Vout)
	}
	if initBack.Time != init.Time {
		t.Fatal(initBack.Time, init.Time)
	}
	if !bytes.Equal(initBack.Contract, init.Contract) {
		t.Fatal(initBack.Contract, init.Contract)
	}
}

func TestAudit(t *testing.T) {
	// serialization: orderid (32) + matchid (32) + time (8) + contract (97 ish)
	oid, _ := BytesFromHex("d6c752bb34d833b6e0eb4d114d690d044f8ab3f6de9defa08e9d7d237f670fe4")
	mid, _ := BytesFromHex("79f84ef6c60e72edd305047c015d7b7ade64525a301fdac136976f05edb6172b")
	contract, _ := BytesFromHex("fc99f576f8e0e5dc")
	audit := &Audit{
		OrderID:  oid,
		MatchID:  mid,
		Time:     1570705920,
		Contract: contract,
	}

	exp := []byte{
		// Order ID 32 bytes
		0xd6, 0xc7, 0x52, 0xbb, 0x34, 0xd8, 0x33, 0xb6, 0xe0, 0xeb, 0x4d, 0x11,
		0x4d, 0x69, 0x0d, 0x04, 0x4f, 0x8a, 0xb3, 0xf6, 0xde, 0x9d, 0xef, 0xa0,
		0x8e, 0x9d, 0x7d, 0x23, 0x7f, 0x67, 0x0f, 0xe4,
		// Match ID 32 bytes
		0x79, 0xf8, 0x4e, 0xf6, 0xc6, 0x0e, 0x72, 0xed, 0xd3, 0x05, 0x04, 0x7c,
		0x01, 0x5d, 0x7b, 0x7a, 0xde, 0x64, 0x52, 0x5a, 0x30, 0x1f, 0xda, 0xc1,
		0x36, 0x97, 0x6f, 0x05, 0xed, 0xb6, 0x17, 0x2b,
		// Timestamp 8 bytes
		0x00, 0x00, 0x00, 0x00, 0x5d, 0x9f, 0x12, 0x00,
		// Contract 8 bytes (shortened for testing)
		0xfc, 0x99, 0xf5, 0x76, 0xf8, 0xe0, 0xe5, 0xdc,
	}

	b, err := audit.Serialize()
	if err != nil {
		t.Fatalf("serialization error: %v", err)
	}
	if !bytes.Equal(b, exp) {
		t.Fatalf("unexpected serialization. Wanted %x, got %x", exp, b)
	}

	auditB, err := json.Marshal(audit)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var auditBack Audit
	err = json.Unmarshal(auditB, &auditBack)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if !bytes.Equal(auditBack.MatchID, audit.MatchID) {
		t.Fatal(auditBack.MatchID, audit.MatchID)
	}
	if !bytes.Equal(auditBack.OrderID, audit.OrderID) {
		t.Fatal(auditBack.OrderID, audit.OrderID)
	}
	if auditBack.Time != audit.Time {
		t.Fatal(auditBack.Time, audit.Time)
	}
	if !bytes.Equal(auditBack.Contract, audit.Contract) {
		t.Fatal(auditBack.Contract, audit.Contract)
	}
}

func TestRevokeMatch(t *testing.T) {
	// serialization: order id (32) + match id (32)
	oid, _ := BytesFromHex("47b903b6e71a1fff3ec1be25b23228bf2e8682b1502dc451f7a9aa32556123f2")
	mid, _ := BytesFromHex("be218305e71b07a11c59c1b6c3ad3cf6ad4ed7582da8c639b87188aa95795c16")
	revoke := &RevokeMatch{
		OrderID: oid,
		MatchID: mid,
	}

	exp := []byte{
		// Order ID 32 bytes
		0x47, 0xb9, 0x03, 0xb6, 0xe7, 0x1a, 0x1f, 0xff, 0x3e, 0xc1, 0xbe, 0x25,
		0xb2, 0x32, 0x28, 0xbf, 0x2e, 0x86, 0x82, 0xb1, 0x50, 0x2d, 0xc4, 0x51,
		0xf7, 0xa9, 0xaa, 0x32, 0x55, 0x61, 0x23, 0xf2,
		// Match ID 32 bytes
		0xbe, 0x21, 0x83, 0x05, 0xe7, 0x1b, 0x07, 0xa1, 0x1c, 0x59, 0xc1, 0xb6,
		0xc3, 0xad, 0x3c, 0xf6, 0xad, 0x4e, 0xd7, 0x58, 0x2d, 0xa8, 0xc6, 0x39,
		0xb8, 0x71, 0x88, 0xaa, 0x95, 0x79, 0x5c, 0x16,
	}

	b, err := revoke.Serialize()
	if err != nil {
		t.Fatalf("serialization error: %v", err)
	}
	if !bytes.Equal(b, exp) {
		t.Fatalf("unexpected serialization. Wanted %x, got %x", exp, b)
	}

	revB, err := json.Marshal(revoke)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var revokeBack RevokeMatch
	err = json.Unmarshal(revB, &revokeBack)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if !bytes.Equal(revokeBack.MatchID, revoke.MatchID) {
		t.Fatal(revokeBack.MatchID, revoke.MatchID)
	}
	if !bytes.Equal(revokeBack.OrderID, revoke.OrderID) {
		t.Fatal(revokeBack.OrderID, revoke.OrderID)
	}
}

func TestRedeem(t *testing.T) {
	// serialization: orderid (32) + matchid (32) + txid (probably 64) + vout (4)
	// + timestamp (8)
	oid, _ := BytesFromHex("ee17139af2d86bd6052829389c0531f71042ed0b0539e617213a9a7151215a1b")
	mid, _ := BytesFromHex("6ea1227b03d7bf05ce1e23f3edf57368f69ba9ee0cc069f09ab0952a36d964c5")
	redeem := &Redeem{
		OrderID: oid,
		MatchID: mid,
		TxID:    "28cb86e678f647cc88da734eed11286dab18b8483feb04580e3cbc90555a0047",
		Vout:    155,
		Time:    1570706834,
	}

	exp := []byte{
		// Order ID 32 bytes
		0xee, 0x17, 0x13, 0x9a, 0xf2, 0xd8, 0x6b, 0xd6, 0x05, 0x28, 0x29, 0x38,
		0x9c, 0x05, 0x31, 0xf7, 0x10, 0x42, 0xed, 0x0b, 0x05, 0x39, 0xe6, 0x17,
		0x21, 0x3a, 0x9a, 0x71, 0x51, 0x21, 0x5a, 0x1b,
		// Match ID 32 bytes
		0x6e, 0xa1, 0x22, 0x7b, 0x03, 0xd7, 0xbf, 0x05, 0xce, 0x1e, 0x23, 0xf3,
		0xed, 0xf5, 0x73, 0x68, 0xf6, 0x9b, 0xa9, 0xee, 0x0c, 0xc0, 0x69, 0xf0,
		0x9a, 0xb0, 0x95, 0x2a, 0x36, 0xd9, 0x64, 0xc5,
		// Trancaction ID 64 bytes
		0x32, 0x38, 0x63, 0x62, 0x38, 0x36, 0x65, 0x36, 0x37, 0x38, 0x66, 0x36,
		0x34, 0x37, 0x63, 0x63, 0x38, 0x38, 0x64, 0x61, 0x37, 0x33, 0x34, 0x65,
		0x65, 0x64, 0x31, 0x31, 0x32, 0x38, 0x36, 0x64, 0x61, 0x62, 0x31, 0x38,
		0x62, 0x38, 0x34, 0x38, 0x33, 0x66, 0x65, 0x62, 0x30, 0x34, 0x35, 0x38,
		0x30, 0x65, 0x33, 0x63, 0x62, 0x63, 0x39, 0x30, 0x35, 0x35, 0x35, 0x61,
		0x30, 0x30, 0x34, 0x37,
		// Vout 4 bytes
		0x00, 0x00, 0x00, 0x9b,
		// Timestamp 8 bytes
		0x00, 0x00, 0x00, 0x00, 0x5d, 0x9f, 0x15, 0x92,
	}

	b, err := redeem.Serialize()
	if err != nil {
		t.Fatalf("serialization error: %v", err)
	}
	if !bytes.Equal(b, exp) {
		t.Fatalf("unexpected serialization. Wanted %x, got %x", exp, b)
	}

	redeemB, err := json.Marshal(redeem)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var redeemBack Redeem
	err = json.Unmarshal(redeemB, &redeemBack)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if !bytes.Equal(redeemBack.MatchID, redeem.MatchID) {
		t.Fatal(redeemBack.MatchID, redeem.MatchID)
	}
	if !bytes.Equal(redeemBack.OrderID, redeem.OrderID) {
		t.Fatal(redeemBack.OrderID, redeem.OrderID)
	}
	if redeemBack.TxID != redeem.TxID {
		t.Fatal(redeemBack.TxID, redeem.TxID)
	}
	if redeemBack.Vout != redeem.Vout {
		t.Fatal(redeemBack.Vout, redeem.Vout)
	}
	if redeemBack.Time != redeem.Time {
		t.Fatal(redeemBack.Time, redeem.Time)
	}
}

func TestSignable(t *testing.T) {
	sig := []byte{
		0x07, 0xad, 0x7f, 0x33, 0xc5, 0xb0, 0x13, 0xa1, 0xbb, 0xd6, 0xad, 0xc0,
		0xd2, 0x16, 0xd8, 0x93, 0x8c, 0x73, 0x64, 0xe5, 0x6a, 0x17, 0x8c, 0x7a,
		0x17, 0xa9, 0xe7, 0x47, 0xad, 0x55, 0xaf, 0xe6, 0x55, 0x2b, 0xb2, 0x76,
		0xf8, 0x8e, 0x34, 0x2e, 0x56, 0xac, 0xaa, 0x8a, 0x52, 0x41, 0x2e, 0x51,
		0x8b, 0x0f, 0xe6, 0xb2, 0x2a, 0x21, 0x77, 0x9a, 0x76, 0x99, 0xa5, 0xe5,
		0x39, 0xa8, 0xa1, 0xdd, 0x1d, 0x49, 0x8b, 0xb0, 0x16, 0xf7, 0x18, 0x70,
	}
	s := signable{}
	s.SetSig(sig)
	if !bytes.Equal(sig, s.SigBytes()) {
		t.Fatalf("signatures not equal")
	}
}

func TestBytes(t *testing.T) {
	rawB := []byte{0xfc, 0xf6, 0xd9, 0xb9, 0xdb, 0x10, 0x4c, 0xc0, 0x13, 0x3a}
	hexB := "fcf6d9b9db104cc0133a"

	type byter struct {
		B Bytes `json:"b"`
	}

	b := byter{}
	js := `{"b":"` + hexB + `"}`
	err := json.Unmarshal([]byte(js), &b)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if !bytes.Equal(rawB, b.B) {
		t.Fatalf("unmarshalled Bytes not correct. wanted %x, got %x.", rawB, b.B)
	}

	marshalled, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if string(marshalled) != js {
		t.Fatalf("marshalled Bytes not correct. wanted %s, got %s", js, string(marshalled))
	}

	fromHex, _ := BytesFromHex(hexB)
	if !bytes.Equal(rawB, fromHex) {
		t.Fatalf("hex-constructed Bytes not correct. wanted %x, got %x.", rawB, fromHex)
	}
}

func TestDecodeMessage(t *testing.T) {
	msg, err := DecodeMessage([]byte(`{"type":1,"route":"testroute","id":5,"payload":10}`))
	if err != nil {
		t.Fatalf("error decoding json message: %v", err)
	}
	if msg.Type != 1 {
		t.Fatalf("wrong message type. wanted 1, got %d", msg.Type)
	}
	if msg.Route != "testroute" {
		t.Fatalf("wrong message type. wanted 'testroute', got '%s'", msg.Route)
	}
	if msg.ID != 5 {
		t.Fatalf("wrong message type. wanted 5, got %d", msg.ID)
	}
	if string(msg.Payload) != "10" {
		t.Fatalf("wrong payload. wanted '10, got '%s'", string(msg.Payload))
	}
	// Test invalid json
	_, err = DecodeMessage([]byte(`{"type":?}`))
	if err == nil {
		t.Fatalf("no json decode error for invalid json")
	}
}

func TestRespReq(t *testing.T) {
	// Test invalid json result.
	_, err := NewResponse(5, make(chan int), nil)
	if err == nil {
		t.Fatalf("no error for invalid json")
	}
	// Zero ID not valid.
	_, err = NewResponse(0, 10, nil)
	if err == nil {
		t.Fatalf("no error for id = 0")
	}
	msg, err := NewResponse(5, 10, nil)
	if err != nil {
		t.Fatalf("NewResponse error: %v", err)
	}
	if msg.ID != 5 {
		t.Fatalf("wrong message ID. wanted 5, got ")
	}
	resp, err := msg.Response()
	if err != nil {
		t.Fatalf("error getting response payload: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error making success response")
	}
	if string(resp.Result) != "10" {
		t.Fatalf("unexpected result. wanted '10', got '%s'", string(resp.Result))
	}

	// Check error.
	msg, err = NewResponse(5, nil, NewError(15, "testmsg"))
	if err != nil {
		t.Fatalf("unexpected error making error response")
	}
	_, err = msg.Response()
	if err != nil {
		t.Fatalf("unexpected error getting error response payload: %v", err)
	}

	// Test Requests
	_, err = NewRequest(5, "testroute", make(chan int))
	if err == nil {
		t.Fatalf("no error for invalid json type request payload")
	}
	_, err = NewRequest(0, "testroute", 10)
	if err == nil {
		t.Fatalf("no error id = 0 request")
	}
	_, err = NewRequest(5, "", 10)
	if err == nil {
		t.Fatalf("no error for empty string route request")
	}
	msg, err = NewRequest(5, "testroute", 10)
	if err != nil {
		t.Fatalf("error for valid request payload: %v", err)
	}
	// A Request-type Message should error if trying to retreive the
	// ResponsePayload
	_, err = msg.Response()
	if err == nil {
		t.Fatalf("no error when retreiving response payload from request-type message")
	}
}
