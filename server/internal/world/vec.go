// Package world is the authoritative simulation (brief §3, M2 scope):
// zones, entities, movement validation with speed clamps, and a uniform-grid
// AOI that drives 20 Hz WorldSnapshot deltas. All state is owned by the
// simulation goroutine; external entry points are synchronous and lock mu.
package world

import (
	"math"
)

// Vec3 is a world-space position. Y is up (Godot convention).
type Vec3 struct {
	X, Y, Z float64
}

// Distance returns the horizontal (XZ) distance between two positions.
func (v Vec3) Distance(o Vec3) float64 {
	dx, dz := v.X-o.X, v.Z-o.Z
	return math.Sqrt(dx*dx + dz*dz)
}

// Add returns v + o.
func (v Vec3) Add(o Vec3) Vec3 { return Vec3{v.X + o.X, v.Y + o.Y, v.Z + o.Z} }

// Mul scales each component by f.
func (v Vec3) Mul(f float64) Vec3 { return Vec3{v.X * f, v.Y * f, v.Z * f} }

// Sub returns v - o.
func (v Vec3) Sub(o Vec3) Vec3 { return Vec3{v.X - o.X, v.Y - o.Y, v.Z - o.Z} }

// Len returns the length of the vector.
func (v Vec3) Len() float64 { return math.Sqrt(v.X*v.X + v.Y*v.Y + v.Z*v.Z) }

// Normalize returns a unit vector (zero vector stays zero).
func (v Vec3) Normalize() Vec3 {
	l := v.Len()
	if l == 0 {
		return Vec3{}
	}
	return Vec3{v.X / l, v.Y / l, v.Z / l}
}
