package uploadguard

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func limits(max int64, kinds ...Kind) Limits {
	return Limits{MaxBytes: max, AllowedKinds: kinds}
}

func TestCheckAcceptsRealPNG(t *testing.T) {
	png := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, []byte("IHDR....")...)
	kind, err := Check(bytes.NewReader(png), limits(1024, KindPNG, KindJPEG))
	if err != nil || kind != KindPNG {
		t.Fatalf("want png, got %q err=%v", kind, err)
	}
}

func TestCheckRejectsExecutableDisguisedAsImage(t *testing.T) {
	// A Windows PE renamed to cat.png — extension and declared type
	// are irrelevant; the MZ magic gets it refused.
	pe := append([]byte{'M', 'Z'}, make([]byte, 64)...)
	_, err := Check(bytes.NewReader(pe), limits(1024, KindPNG))
	if !errors.Is(err, ErrExecutable) {
		t.Fatalf("want ErrExecutable, got %v", err)
	}
}

func TestCheckRejectsELFAndShebang(t *testing.T) {
	for _, payload := range [][]byte{
		{0x7F, 'E', 'L', 'F', 2, 1, 1},
		[]byte("#!/bin/sh\nrm -rf /\n"),
		{'P', 'K', 0x03, 0x04, 0x14}, // zip/jar
	} {
		if _, err := Check(bytes.NewReader(payload), limits(1024, KindCSV, KindPNG)); !errors.Is(err, ErrExecutable) {
			t.Fatalf("payload %v: want ErrExecutable, got %v", payload[:2], err)
		}
	}
}

func TestCheckRejectsCorruptedFile(t *testing.T) {
	// Random binary noise that matches no allowed kind.
	noise := []byte{0x01, 0x02, 0xFF, 0xFE, 0x00, 0x99}
	_, err := Check(bytes.NewReader(noise), limits(1024, KindPNG, KindPDF, KindCSV))
	if !errors.Is(err, ErrUnknownType) {
		t.Fatalf("want ErrUnknownType, got %v", err)
	}
}

func TestCheckRejectsOversize(t *testing.T) {
	big := strings.Repeat("a", 100)
	_, err := Check(strings.NewReader(big), limits(64, KindCSV))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestCheckRejectsEmpty(t *testing.T) {
	_, err := Check(strings.NewReader(""), limits(64, KindCSV))
	if !errors.Is(err, ErrEmpty) {
		t.Fatalf("want ErrEmpty, got %v", err)
	}
}

func TestCheckCSVRequiresPrintableText(t *testing.T) {
	if _, err := Check(strings.NewReader("id,name\n1,alpha\n"), limits(1024, KindCSV)); err != nil {
		t.Fatalf("valid csv refused: %v", err)
	}
	// Binary blob renamed .csv — NUL bytes give it away.
	if _, err := Check(bytes.NewReader([]byte("id,na\x00me\n")), limits(1024, KindCSV)); !errors.Is(err, ErrUnknownType) {
		t.Fatalf("want ErrUnknownType for NUL-laced csv, got %v", err)
	}
}

func TestCheckJSONShape(t *testing.T) {
	if _, err := Check(strings.NewReader(`  {"a":1}`), limits(1024, KindJSON)); err != nil {
		t.Fatalf("valid json refused: %v", err)
	}
	if _, err := Check(strings.NewReader("not json"), limits(1024, KindJSON)); !errors.Is(err, ErrUnknownType) {
		t.Fatalf("want ErrUnknownType, got %v", err)
	}
}
