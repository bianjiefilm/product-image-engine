// Package platelock turns a verified model background plate plus a frozen
// sample into a derived composite and a server-side quality report. It is pure:
// no I/O, no clocks, no randomness, and no input that could carry a score or a
// "passed" claim from the browser.
package platelock

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"

	xdraw "golang.org/x/image/draw"
)

const (
	// FitID names the canvas adaptation recorded in every report.
	FitID = "fit-cover-center-catmullrom/v1"
	// ComposeID names the subject lock recorded in every report.
	ComposeID = "compose-mask-lock/v1"
	// PlateSide is the only plate size the priced SKU produces.
	PlateSide = 1024
)

var ErrImage = errors.New("platelock: image cannot be decoded as PNG")

// decode returns a zero-origin NRGBA copy so every pixel loop is a direct index.
func decode(raw []byte) (*image.NRGBA, error) {
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrImage
	}
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			out.SetNRGBA(x, y, color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA))
		}
	}
	return out, nil
}

func encode(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// FitPlateCover scales the plate uniformly until it covers w x h and crops the
// centre. It never stretches: a 1024x1024 plate on a 1024x1280 canvas becomes
// 1280x1280 (CatmullRom) then loses 128 columns on each side; on 768x1024 it is
// only cropped. The result is a PNG exactly w x h.
func FitPlateCover(plate []byte, w, h int) ([]byte, error) {
	if w < 1 || h < 1 || w > 4096 || h > 4096 {
		return nil, ErrImage
	}
	src, err := decode(plate)
	if err != nil {
		return nil, err
	}
	pw, ph := src.Bounds().Dx(), src.Bounds().Dy()
	if pw < 1 || ph < 1 {
		return nil, ErrImage
	}
	var sw, sh int
	if w*ph >= h*pw { // width drives the scale
		sw, sh = w, (ph*w+pw-1)/pw
	} else {
		sh, sw = h, (pw*h+ph-1)/ph
	}
	var scaled *image.NRGBA
	if sw == pw && sh == ph {
		scaled = src
	} else {
		scaled = image.NewNRGBA(image.Rect(0, 0, sw, sh))
		xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	}
	x0, y0 := (sw-w)/2, (sh-h)/2
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out.SetNRGBA(x, y, scaled.NRGBAAt(x0+x, y0+y))
		}
	}
	return encode(out)
}

// Compose copies original pixels where the mask protects them (alpha >= 128)
// and plate pixels elsewhere. It is bgreplace.LockSubject without its two
// refusals, because "no background change" and "no protected pixel" must be
// reported as failed axes with a record, not swallowed as an error.
func Compose(original, fittedPlate, mask []byte) ([]byte, error) {
	o, err := decode(original)
	if err != nil {
		return nil, err
	}
	p, err := decode(fittedPlate)
	if err != nil {
		return nil, err
	}
	m, err := decode(mask)
	if err != nil {
		return nil, err
	}
	b := o.Bounds()
	if p.Bounds().Dx() != b.Dx() || p.Bounds().Dy() != b.Dy() || m.Bounds().Dx() != b.Dx() || m.Bounds().Dy() != b.Dy() {
		return nil, ErrImage
	}
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			if m.NRGBAAt(x, y).A >= 128 {
				out.SetNRGBA(x, y, o.NRGBAAt(x, y))
			} else {
				out.SetNRGBA(x, y, p.NRGBAAt(x, y))
			}
		}
	}
	return encode(out)
}
