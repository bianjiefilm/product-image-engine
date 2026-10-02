package samplegen

import "image/color"

// MinDirectionContrast is the smallest Euclidean RGB distance allowed between a frozen
// background direction's representative colour and the sample's own background (R30).
const MinDirectionContrast = 100.0

// DirectionReference is the representative mean colour of what each frozen direction
// describes. It exists only to pin R30 in tests; it is not read at runtime, and a
// direction added to `kinds` without an entry here fails TestEveryFrozenDirectionContrasts….
var DirectionReference = map[string]color.NRGBA{
	"深蓝色渐变摄影棚背景，柔和顶光": {20, 40, 90, 255},
	"暖色原木桌面，窗边自然光":    {150, 100, 60, 255},
	"深灰色大理石台面，冷色侧光":   {70, 70, 75, 255},
	"绿色植物墙背景，浅景深":     {50, 110, 60, 255},
	"黑色亚克力台面，细长高光":    {20, 20, 22, 255},
	"海边沙滩与蓝天，明亮日光":    {110, 170, 215, 255},
}
