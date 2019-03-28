package adv

import (
	"fmt"
	"reflect"
	"testing"
)

type testPdu struct {
	b []byte
}

func (t *testPdu) addBad(recTyp byte, badRecLen byte, recBytes []byte) {
	t.b = append(t.b, badRecLen, recTyp)
	t.b = append(t.b, recBytes...)
}

func (t *testPdu) add(recTyp byte, recBytes []byte) {
	lb := byte(len(recBytes) + 1)
	t.b = append(t.b, lb, recTyp)
	t.b = append(t.b, recBytes...)
}

func (t *testPdu) bytes() []byte {
	return t.b
}

func testArrayBad(typ byte, t *testing.T) error {
	dec, ok := pduDecodeMap[typ]
	if !ok || dec.arrayElementSz == 0 {
		t.Fatalf("unsupported type %v", typ)
	}

	//len == 0
	p := testPdu{}
	b := []byte{}
	p.add(typ, b)

	_, err := decode(p.bytes())
	if err == nil {
		return fmt.Errorf("len==0, no decode error")
	}

	//len % arraySz != 0
	p = testPdu{}
	b1 := []byte{}
	b2 := []byte{}
	for i := 0; i < dec.arrayElementSz; i++ {
		bi := byte(i)
		b1 = append(b1, bi)
		b2 = append(b2, 255-bi)
	}

	b = append(b1, b2...)
	b = append(b, 0xbb) //appending extra byte here!
	p.add(typ, b)

	_, err = decode(p.bytes())
	if err == nil {
		return fmt.Errorf("len%%size != 0, no decode error")
	}

	// len < elementSz
	p = testPdu{}
	b = []byte{}
	for i := 0; i < (dec.arrayElementSz - 1); i++ { //-1 for error
		bi := byte(i)
		b1 = append(b1, bi)
	}
	p.add(typ, b)

	_, err = decode(p.bytes())
	if err == nil {
		return fmt.Errorf("len<arrayElementSize, no decode error")
	}

	// len < minSz
	p = testPdu{}
	b = []byte{}
	for i := 0; i < (dec.minSz - 1); i++ { //-1 for error
		bi := byte(i)
		b1 = append(b1, bi)
	}
	p.add(typ, b)

	_, err = decode(p.bytes())
	if err == nil {
		return fmt.Errorf("len<minSz, no decode error")
	}

	//corrupt encoding (bad length)
	p = testPdu{}
	b1 = []byte{}
	b2 = []byte{}

	for i := 0; i < dec.arrayElementSz; i++ {
		bi := byte(i)
		b1 = append(b1, bi)
		b2 = append(b2, 128+bi)
	}

	//+32
	b = append(b1, b2...)
	badLength := byte(len(b) + 32)
	p.addBad(typ, badLength, b)

	_, err = decode(p.bytes())
	if err == nil {
		return fmt.Errorf("corrupt length +32, no decode error")
	}

	//255
	p = testPdu{}
	p.addBad(typ, 255, b)
	_, err = decode(p.bytes())
	if err == nil {
		return fmt.Errorf("corrupt length 255, no decode error")
	}

	return nil
}

func testArrayGood(typ byte, t *testing.T) error {
	dec, ok := pduDecodeMap[typ]
	if !ok || dec.arrayElementSz == 0 {
		t.Fatalf("unsupported type %v", typ)
	}

	p := testPdu{}
	b1 := []byte{}
	b2 := []byte{}
	b3 := []byte{}

	for i := 0; i < dec.arrayElementSz; i++ {
		bi := byte(i)
		b1 = append(b1, bi)
		b2 = append(b2, 128+bi)
		b3 = append(b3, 255-bi)
	}

	b := append(b1, b2...)
	b = append(b, b3...)
	p.add(typ, b)

	m, err := decode(p.bytes())
	if err != nil {
		return fmt.Errorf("decode error %v", err)
	}

	t.Logf("%+v", m)

	v, ok := m[dec.key]
	if !ok {
		return fmt.Errorf("missing key %v", dec.key)
	}

	//check type
	vv, ok := v.([]interface{})
	if !ok {
		return fmt.Errorf("wrong type %v", reflect.TypeOf(v))
	}

	//check the count
	if len(vv) != 3 {
		return fmt.Errorf("uuid count mismatch, exp 3, have %v", len(vv))
	}

	//check contents
	ok = reflect.DeepEqual(vv[0], b1)
	if !ok {
		return fmt.Errorf("mismatch @ 0")
	}
	ok = reflect.DeepEqual(vv[1], b2)
	if !ok {
		return fmt.Errorf("mismatch @ 1")
	}
	ok = reflect.DeepEqual(vv[2], b3)
	if !ok {
		return fmt.Errorf("mismatch @ 2")
	}

	return nil
}

func TestParserArrays(t *testing.T) {
	types := []byte{
		types.uuid16inc,
		types.uuid16comp,
		types.uuid32inc,
		types.uuid32comp,
		types.uuid128inc,
		types.uuid128comp,
		types.sol16,
		types.sol32,
		types.sol128,
	}

	for _, v := range types {
		err := testArrayGood(v, t)
		t.Logf("adv type %v, testArrayGood err %v", v, err)
		if err != nil {
			t.Fatal(err)
		}

		err = testArrayBad(v, t)
		t.Logf("adv type %v, testArrayBad err %v", v, err)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func testNonArrayGood(typ byte, t *testing.T) error {
	dec, ok := pduDecodeMap[typ]
	if !ok || dec.arrayElementSz != 0 {
		t.Fatalf("unsupported type %v", typ)
	}

	p := testPdu{}
	b := []byte{}
	for i := 0; i < dec.minSz; i++ {
		bi := byte(i)
		b = append(b, bi)
	}

	p.add(typ, b)
	m, err := decode(p.bytes())
	if err != nil {
		return fmt.Errorf("decode error %v", err)
	}

	t.Logf("%+v", m)
	v, ok := m[dec.key]
	if !ok {
		return fmt.Errorf("missing key %v", dec.key)
	}

	vv, ok := v.(interface{})
	if !ok {
		return fmt.Errorf("wrong type %v", reflect.TypeOf(v))
	}

	ok = reflect.DeepEqual(vv, b)
	if !ok {
		return fmt.Errorf("mismatch")
	}

	return nil
}

func testNonArrayBad(typ byte, t *testing.T) error {
	dec, ok := pduDecodeMap[typ]
	if !ok || dec.arrayElementSz != 0 {
		t.Fatalf("unsupported type %v", typ)
	}

	//len == 0
	p := testPdu{}
	b := []byte{}
	p.add(typ, b)

	_, err := decode(p.bytes())
	if err == nil {
		return fmt.Errorf("len==0, no decode error")
	}

	// len < minSz (may also cover len == 0)
	p = testPdu{}
	b = []byte{}
	for i := 0; i < (dec.minSz - 1); i++ { //-1 for error
		bi := byte(i)
		b = append(b, bi)
	}
	p.add(typ, b)

	_, err = decode(p.bytes())
	if err == nil {
		return fmt.Errorf("len<minSz, no decode error")
	}

	//corrupt encoding (bad length)
	p = testPdu{}
	b = []byte{}

	for i := 0; i < dec.arrayElementSz; i++ {
		bi := byte(i)
		b = append(b, bi)
	}

	//+32
	badLength := byte(len(b) + 32)
	p.addBad(typ, badLength, b)

	_, err = decode(p.bytes())
	if err == nil {
		return fmt.Errorf("corrupt length +32, no decode error")
	}

	//255
	p = testPdu{}
	p.addBad(typ, 255, b)
	_, err = decode(p.bytes())
	if err == nil {
		return fmt.Errorf("corrupt length 255, no decode error")
	}

	return nil
}

func TestParserNonArrays(t *testing.T) {
	types := []byte{
		types.flags,
		types.nameshort,
		types.namecomp,
		types.txpwr,
		types.mfgdata,
	}

	for _, v := range types {
		err := testNonArrayGood(v, t)
		t.Logf("adv type %v, testArrayGood err %v", v, err)
		if err != nil {
			t.Fatal(err)
		}

		err = testNonArrayBad(v, t)
		t.Logf("adv type %v, testArrayBad err %v", v, err)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestParserCombined(t *testing.T) {
	//uuid16 + uuid128 + flags
	u16 := []byte{1, 2, 3, 4}
	u128 := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	flags := []byte{0xcd}

	p := testPdu{}
	p.add(types.flags, flags)
	p.add(types.uuid16comp, u16)
	p.add(types.uuid128comp, u128)

	m, err := decode(p.bytes())
	if err != nil {
		t.Fatalf("combined adv decode err %v", err)
	}

	mu16, mu16ok := m["uuid16"]
	mu128, mu128ok := m["uuid128"]
	mf, mfok := m["flags"]

	if !mu16ok || !mu128ok || !mfok {
		t.Fatalf("decoded map is missing a key")
	}

	//check flag
	switch mf.(type) {
	case interface{}:
		ok := reflect.DeepEqual(mf.(interface{}), flags)
		if !ok {
			t.Fatalf("flag mismatch at idx 0")
		}

	default:
		t.Fatalf("got invalid flag type, %v", reflect.TypeOf(mu16))
	}

	//check uuid16
	switch mu16.(type) {
	case []interface{}:
		ok0 := reflect.DeepEqual(mu16.([]interface{})[0], u16[0:2])
		if !ok0 {
			t.Fatalf("uuid16 mismatch at idx 0")
		}
		ok1 := reflect.DeepEqual(mu16.([]interface{})[1], u16[2:])
		if !ok1 {
			t.Fatalf("uuid16 mismatch at idx 1")
		}

		if len(mu16.([]interface{})) != 2 {
			t.Fatalf("uuid128 count != 2")
		}

	default:
		t.Fatalf("got invalid uuid16 type, %v", reflect.TypeOf(mu16))
	}

	//check uuid128
	switch mu128.(type) {
	case []interface{}:
		ok := reflect.DeepEqual(mu128.([]interface{})[0], u128)
		if !ok {
			t.Fatalf("uuid128 mismatch at idx 0")
		}

		if len(mu128.([]interface{})) != 1 {
			t.Fatalf("uuid128 count != 1")
		}

	default:
		t.Fatalf("got invalid uuid128 type, %v", reflect.TypeOf(mu16))
	}
}
