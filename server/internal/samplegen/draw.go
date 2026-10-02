// Package samplegen deterministically draws the frozen synthetic product
// samples. It uses only integer arithmetic and the fixed basicfont bitmap, so
// the same pixels are reproduced on every machine and Go version.
package samplegen

import (
	"image"
	"image/color"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	fs "github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
)

func fillRect(img *image.NRGBA, r fs.Rect, c color.NRGBA) {
	for y := r[1]; y < r[3]; y++ {
		for x := r[0]; x < r[2]; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
}

// fillDisk draws a filled disk using integer distance, clipped by optional minX.
func fillDisk(img *image.NRGBA, cx, cy, r int, c color.NRGBA) {
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			if (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r {
				img.SetNRGBA(x, y, c)
			}
		}
	}
}

// drawText renders s with the 7x13 bitmap face scaled by an integer factor and
// returns the exact box it occupies.
func drawText(img *image.NRGBA, s string, x, y, scale int, c color.NRGBA) fs.Rect {
	w, h := len(s)*7, 13
	tmp := image.NewNRGBA(image.Rect(0, 0, w, h))
	d := &font.Drawer{Dst: tmp, Src: image.NewUniform(color.NRGBA{255, 255, 255, 255}), Face: basicfont.Face7x13, Dot: fixed.P(0, 11)}
	d.DrawString(s)
	for ty := 0; ty < h; ty++ {
		for tx := 0; tx < w; tx++ {
			if tmp.NRGBAAt(tx, ty).A == 0 {
				continue
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.SetNRGBA(x+tx*scale+dx, y+ty*scale+dy, c)
				}
			}
		}
	}
	return fs.Rect{x, y, x + w*scale, y + h*scale}
}

type drawn struct {
	original, mask *image.NRGBA
	cov            fs.Coverage
}

func newPair(w, h int, bg color.NRGBA) (*image.NRGBA, *image.NRGBA) {
	o := image.NewNRGBA(image.Rect(0, 0, w, h))
	fillRect(o, fs.Rect{0, 0, w, h}, bg)
	m := image.NewNRGBA(image.Rect(0, 0, w, h)) // fully transparent
	return o, m
}

var opaqueMask = color.NRGBA{255, 255, 255, 255}

func drawCarton(w, h int) drawn {
	u := min(w, h) / 16
	o, m := newPair(w, h, color.NRGBA{226, 222, 214, 255})
	cx, cy := w/2, h/2
	hw, hh := 7*u/2, 9*u/2
	body := fs.Rect{cx - hw, cy - hh, cx + hw, cy + hh}
	fillRect(o, body, color.NRGBA{196, 60, 48, 255})
	fillRect(m, body, opaqueMask)
	flap := fs.Rect{body[0], body[1], body[2], body[1] + u}
	fillRect(o, flap, color.NRGBA{150, 40, 34, 255})
	fillRect(o, fs.Rect{body[0], flap[3], body[2], flap[3] + 2}, color.NRGBA{110, 30, 26, 255})
	panel := fs.Rect{body[0] + u, body[1] + u + u/4, body[2] - u, body[3] - u}
	fillRect(o, panel, color.NRGBA{246, 244, 238, 255})
	ly := panel[1] + u + u/4
	fillDisk(o, cx, ly, u, color.NRGBA{196, 60, 48, 255})
	fillDisk(o, cx, ly, u/2, color.NRGBA{246, 244, 238, 255})
	logo := fs.Rect{cx - u, ly - u, cx + u + 1, ly + u + 1}
	s := u / 12
	s2 := (s + 1) / 2
	s3 := (s2 + 1) / 2
	ink := color.NRGBA{34, 30, 28, 255}
	tx := drawText(o, "PAINUO", cx-6*7*s/2, panel[1]+2*u+u/2, s, ink)
	sp := drawText(o, "NET 250G", cx-8*7*s2/2, tx[3]+u/4, s2, ink)
	lot := drawText(o, "LOT 20261002", cx-12*7*s3/2, sp[3]+u/4, s3, ink)
	return drawn{o, m, fs.Coverage{
		Logo: []fs.Rect{logo}, PackagingText: []fs.Rect{tx, lot}, Spec: []fs.Rect{sp},
		Structure: []fs.Rect{flap, {body[0], body[3] - u/2, body[2], body[3]}},
	}}
}

var steel = [8]uint8{120, 170, 215, 235, 225, 190, 150, 125}

func drawHandledMetal(w, h int) drawn {
	u := min(w, h) / 16
	o, m := newPair(w, h, color.NRGBA{214, 220, 226, 255})
	cx, cy := w/2, h/2
	x0 := cx - 7*u/2
	body := fs.Rect{x0, cy - 3*u, x0 + 5*u, cy + 3*u}
	bw := body[2] - body[0]
	for x := body[0]; x < body[2]; x++ {
		v := steel[(x-body[0])*8/bw]
		fillRect(o, fs.Rect{x, body[1], x + 1, body[3]}, color.NRGBA{v, v + 4, v + 10, 255})
	}
	fillRect(m, body, opaqueMask)
	rim := fs.Rect{body[0], body[1], body[2], body[1] + u/2}
	fillRect(o, rim, color.NRGBA{240, 244, 248, 255})
	hx, hy, big, small := body[2], cy, 2*u, u+u/4
	for y := hy - big; y <= hy+big; y++ {
		for x := hx; x <= hx+big; x++ {
			d := (x-hx)*(x-hx) + (y-hy)*(y-hy)
			if d <= big*big && d >= small*small {
				o.SetNRGBA(x, y, color.NRGBA{190, 196, 204, 255})
				m.SetNRGBA(x, y, opaqueMask)
			}
		}
	}
	s := u / 12
	s2 := (s + 1) / 2
	ink := color.NRGBA{52, 56, 64, 255}
	ly := body[1] + u + u/2
	fillDisk(o, (body[0]+body[2])/2, ly, u/2, ink)
	fillDisk(o, (body[0]+body[2])/2, ly, u/4, color.NRGBA{225, 230, 236, 255})
	logo := fs.Rect{(body[0]+body[2])/2 - u/2, ly - u/2, (body[0]+body[2])/2 + u/2 + 1, ly + u/2 + 1}
	tx := drawText(o, "PAINUO", (body[0]+body[2])/2-6*7*s/2, body[1]+2*u+u/2, s, ink)
	sp := drawText(o, "350ML", (body[0]+body[2])/2-5*7*s2/2, tx[3]+u/2, s2, ink)
	bar := fs.Rect{hx, hy - big + u/8, hx + u/4, hy - small - u/16}
	return drawn{o, m, fs.Coverage{
		Logo: []fs.Rect{logo}, PackagingText: []fs.Rect{tx}, Spec: []fs.Rect{sp},
		Structure: []fs.Rect{rim, bar},
	}}
}

func drawGlassBottle(w, h int) drawn {
	u := min(w, h) / 16
	o, m := newPair(w, h, color.NRGBA{222, 228, 222, 255})
	cx, cy := w/2, h/2
	top := cy - 4*u
	cap := fs.Rect{cx - u, top, cx + u, top + 3*u/4}
	neck := fs.Rect{cx - 3*u/4, cap[3], cx + 3*u/4, cap[3] + 2*u}
	body := fs.Rect{cx - 2*u, neck[3], cx + 2*u, neck[3] + 5*u}
	fillRect(o, cap, color.NRGBA{40, 90, 120, 255})
	fillRect(o, neck, color.NRGBA{176, 214, 210, 255})
	fillRect(o, body, color.NRGBA{190, 226, 222, 255})
	fillRect(o, fs.Rect{body[0], body[1], body[0] + u/8, body[3]}, color.NRGBA{140, 190, 186, 255})
	fillRect(o, fs.Rect{body[2] - u/8, body[1], body[2], body[3]}, color.NRGBA{140, 190, 186, 255})
	fillRect(o, fs.Rect{body[0] + u/4, body[1] + u/4, body[0] + u/2, body[3] - u/4}, color.NRGBA{240, 250, 248, 255})
	for _, r := range []fs.Rect{cap, neck, body} {
		fillRect(m, r, opaqueMask)
	}
	label := fs.Rect{body[0] + u/4 + u/2, body[1] + u, body[2] - u/4, body[3] - u}
	fillRect(o, label, color.NRGBA{246, 246, 240, 255})
	s := u / 16
	s2 := (s + 1) / 2
	ink := color.NRGBA{30, 70, 96, 255}
	lcx := (label[0] + label[2]) / 2
	ly := label[1] + u/8 + u/2
	fillDisk(o, lcx, ly, u/2, color.NRGBA{40, 90, 120, 255})
	fillDisk(o, lcx, ly, u/4, color.NRGBA{246, 246, 240, 255})
	logo := fs.Rect{lcx - u/2, ly - u/2, lcx + u/2 + 1, ly + u/2 + 1}
	tx := drawText(o, "PAINUO", lcx-6*7*s/2, ly+u/2+u/8, s, ink)
	sp := drawText(o, "500ML", lcx-5*7*s2/2, tx[3]+u/8, s2, ink)
	return drawn{o, m, fs.Coverage{
		Logo: []fs.Rect{logo}, PackagingText: []fs.Rect{tx}, Spec: []fs.Rect{sp},
		Structure: []fs.Rect{cap, neck},
	}}
}
