package revision

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"

	"github.com/bianjiefilm/product-image-engine/server/internal/bgreplace"
)

// PlateSource 是整图重画钩子。锁定路径不得调用它。
type PlateSource interface {
	FullRedraw(Intent, []byte) ([]byte, error)
}

// ExecuteRequest 是一次局部修改。FullRedraw 只用于证明没有被调用。
type ExecuteRequest struct {
	Intent       Intent
	Provider     Provider
	Acknowledged bool
	Original     []byte
	Mask         []byte
	FullRedraw   PlateSource
}

// Result 是确定性底板锁回主体后的字节。SupplierCalls 在本构建恒为 0。
type Result struct {
	Plan             Plan
	PNG              []byte
	ContentHash      string
	Origin           string
	SubjectPreserved bool
	StepsRun         []string
	SupplierCalls    int
}

// Execute 在 preserve_subject 时只换蒙版外像素。不调用 FullRedraw，也不调用供应商。
func Execute(req ExecuteRequest) (Result, error) {
	if !req.Intent.PreserveSubject {
		return Result{}, ErrPreserveSubject
	}
	if req.Intent.Action == ActionUnrecognized {
		return Result{}, ErrUnrecognized
	}
	plan, err := PlanRevision(req.Intent, req.Provider)
	if err != nil {
		return Result{}, err
	}
	if plan.RequiresAcknowledgement && !req.Acknowledged {
		return Result{Plan: plan}, ErrImpactUnacknowledged
	}
	if !pixelAction(req.Intent.Action) {
		return Result{Plan: plan, StepsRun: append([]string{}, plan.Steps...), SupplierCalls: 0}, nil
	}
	plate, err := deterministicPlate(req.Original, req.Intent.Action)
	if err != nil {
		return Result{}, err
	}
	locked, err := bgreplace.LockSubject(req.Original, plate, req.Mask)
	if err != nil {
		return Result{Plan: plan}, err
	}
	if _, err := bgreplace.SubjectPixelChecks(req.Original, locked, req.Mask); err != nil {
		return Result{Plan: plan}, err
	}
	sum := sha256.Sum256(locked)
	return Result{
		Plan:             plan,
		PNG:              locked,
		ContentHash:      hex.EncodeToString(sum[:]),
		Origin:           bgreplace.OriginSubjectLock,
		SubjectPreserved: true,
		StepsRun:         []string{StepDeterministicPlate, StepSubjectLock, StepVersionRecord},
		SupplierCalls:    0,
	}, nil
}

func deterministicPlate(original []byte, action string) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(original))
	if err != nil {
		return nil, ErrBadImage
	}
	bounds := src.Bounds()
	if bounds.Dx() < 1 || bounds.Dy() < 1 {
		return nil, ErrBadImage
	}
	fill := plateColor(action)
	out := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			out.SetNRGBA(x, y, fill)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func plateColor(action string) color.NRGBA {
	switch action {
	case ActionOutdoor:
		return color.NRGBA{R: 70, G: 140, B: 210, A: 255}
	case ActionBrightenKeepProduct:
		return color.NRGBA{R: 255, G: 244, B: 220, A: 255}
	default:
		return color.NRGBA{R: 220, G: 220, B: 220, A: 255}
	}
}
