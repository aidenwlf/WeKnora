package imagegen

import (
	"strconv"
	"strings"
)

// CheckSize is the size a connection check asks for: the smallest one the
// model documents as a closed list, or "" to leave the vendor its default
// where it documents no list.
func CheckSize(g ImageGenerator) string {
	if w, ok := g.(*langfuseGenerator); ok {
		g = w.inner
	}
	s, ok := g.(interface{ sizes() []string })
	if !ok {
		return ""
	}
	best, bestArea := "", 0
	for _, size := range s.sizes() {
		if area := pixelArea(size); area > 0 && (bestArea == 0 || area < bestArea) {
			best, bestArea = size, area
		}
	}
	return best
}

// pixelArea reads WIDTHxHEIGHT; keywords such as auto have none.
func pixelArea(size string) int {
	w, h, ok := strings.Cut(strings.ToLower(size), "x")
	if !ok {
		return 0
	}
	width, err1 := strconv.Atoi(w)
	height, err2 := strconv.Atoi(h)
	if err1 != nil || err2 != nil || width <= 0 || height <= 0 {
		return 0
	}
	return width * height
}
