// structs: particle update loop built from small 3D-vector structs that are
// passed and returned by value (value-receiver methods, struct-returning
// functions, whole-struct array element copies). Each particle is a charged
// mass on a spring in a uniform magnetic field, integrated with semi-implicit
// Euler. Measures struct passing/return ABI, inlining, and scalar replacement
// of aggregates. Prints kinetic and potential sums and one coordinate.
package main

import "pluto/fmt"

const NP = 1000
const Steps = 500000

type Vec3 struct {
	X, Y, Z float64
}

func (a Vec3) Add(b Vec3) Vec3 {
	return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z}
}

func (a Vec3) Scale(s float64) Vec3 {
	return Vec3{a.X * s, a.Y * s, a.Z * s}
}

func (a Vec3) Dot(b Vec3) float64 {
	return a.X*b.X + a.Y*b.Y + a.Z*b.Z
}

func (a Vec3) Cross(b Vec3) Vec3 {
	return Vec3{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X}
}

type Particle struct {
	Pos     Vec3
	Vel     Vec3
	InvMass float64
}

var particles [NP]Particle

func force(p Particle, field Vec3, k float64) Vec3 {
	spring := p.Pos.Scale(-k)
	lorentz := p.Vel.Cross(field)
	return spring.Add(lorentz).Scale(p.InvMass)
}

func step(p Particle, field Vec3, k float64, dt float64) Particle {
	acc := force(p, field, k)
	p.Vel = p.Vel.Add(acc.Scale(dt))
	p.Pos = p.Pos.Add(p.Vel.Scale(dt))
	return p
}

func main() int64 {
	for i := 0; i < NP; i++ {
		particles[i] = Particle{
			Pos:     Vec3{float64(i%17) - 8, float64(i%13) - 6, float64(i%11) - 5},
			Vel:     Vec3{(float64(i%7) - 3) * 0.1, (float64(i%5) - 2) * 0.1, (float64(i%3) - 1) * 0.1},
			InvMass: 1.0 / float64(1+i%5),
		}
	}
	field := Vec3{0.0, 0.0, 0.5}
	k := 1.0
	dt := 0.001
	for s := 0; s < Steps; s++ {
		for i := 0; i < NP; i++ {
			particles[i] = step(particles[i], field, k, dt)
		}
	}
	kinetic := 0.0
	potential := 0.0
	for i := 0; i < NP; i++ {
		kinetic += particles[i].Vel.Dot(particles[i].Vel)
		potential += particles[i].Pos.Dot(particles[i].Pos)
	}
	fmt.Printf("%d\n", int64(kinetic*1e6))
	fmt.Printf("%d\n", int64(potential*1e6))
	fmt.Printf("%d\n", int64(particles[NP/2].Pos.X*1e9))
	return 0
}
