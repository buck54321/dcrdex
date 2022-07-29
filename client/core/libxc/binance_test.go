package libxc

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestDecodeStreamUpdates(t *testing.T) {
	updates := [][]byte{
		[]byte(`{"e":"executionReport","E":1659034484347,"s":"DCRUSD","c":"web_49a1cac683d943daa5b633610ccbbd5e","S":"SELL","o":"LIMIT","f":"GTC","q":"1.00000000","p":"41.9626","P":"0.0000","F":"0.00000000","g":-1,"C":"","x":"NEW","X":"NEW","r":"NONE","i":665544591,"l":"0.00000000","z":"0.00000000","L":"0.0000","n":"0","N":null,"T":1659034484346,"t":-1,"I":1346875870,"w":true,"m":false,"M":false,"O":1659034484346,"Z":"0.0000","Y":"0.0000","Q":"0.0000"}`),
		[]byte(`{"e":"executionReport","E":1659034484347,"s":"DCRUSD","c":"web_49a1cac683d943daa5b633610ccbbd5e","S":"SELL","o":"LIMIT","f":"GTC","q":"1.00000000","p":"41.9626","P":"0.0000","F":"0.00000000","g":-1,"C":"","x":"NEW","X":"NEW","r":"NONE","i":665544591,"l":"0.00000000","z":"0.00000000","L":"0.0000","n":"0","N":null,"T":1659034484346,"t":-1,"I":1346875870,"w":true,"m":false,"M":false,"O":1659034484346,"Z":"0.0000","Y":"0.0000","Q":"0.0000"}`),
		[]byte(`{"e":"executionReport","E":1659034484347,"s":"DCRUSD","c":"web_49a1cac683d943daa5b633610ccbbd5e","S":"SELL","o":"LIMIT","f":"GTC","q":"1.00000000","p":"41.9626","P":"0.0000","F":"0.00000000","g":-1,"C":"","x":"TRADE","X":"FILLED","r":"NONE","i":665544591,"l":"1.00000000","z":"1.00000000","L":"41.9687","n":"0.0420","N":"USD","T":1659034484346,"t":20075149,"I":1346875871,"w":false,"m":false,"M":true,"O":1659034484346,"Z":"41.9687","Y":"41.9687","Q":"0.0000"}`),
		[]byte(`{"e":"outboundAccountPosition","E":1659034484347,"u":1659034484346,"B":[{"a":"USD","f":"45.5688","l":"0.0000"},{"a":"BNB","f":"0.00004329","l":"0.00000000"},{"a":"DCR","f":"26.32347000","l":"0.00000000"}]}`),
	}
	for i, b := range updates {
		b = bytes.ReplaceAll(b, []byte(`"E"`), []byte(`"E0"`))
		var u *bncStreamUpdate
		if err := json.Unmarshal(b, &u); err != nil {
			t.Fatalf("%d: %v", i, err)
		}
	}
}
