// nbody: Benchmarks Game n-body (Jovian planets, symplectic integrator).
// Measures float64 arithmetic, sqrt, and field access on an array of structs.
//
// Prints the system energy before and after the run as int64(e * 1e9).
//
// Cross-language note: the solar-mass and days-per-year factors are computed
// at run time from float64 variables. Go evaluates constant expressions with
// exact (arbitrary-precision) arithmetic, while C and Rust round after every
// operation, so a constant expression like 4*pi*pi could differ by one ULP.
package main

import (
	"fmt"
	"math"
)

const Steps = 35000000

type Body struct {
	x, y, z, vx, vy, vz, mass float64
}

var bodies [5]Body

var pi float64 = 3.141592653589793
var daysPerYear float64 = 365.24
var solarMass float64

func initBodies() {
	solarMass = 4 * pi * pi
	// Sun
	bodies[0] = Body{x: 0, y: 0, z: 0, vx: 0, vy: 0, vz: 0, mass: solarMass}
	// Jupiter
	bodies[1] = Body{
		x:    4.84143144246472090e+00,
		y:    -1.16032004402742839e+00,
		z:    -1.03622044471123109e-01,
		vx:   1.66007664274403694e-03 * daysPerYear,
		vy:   7.69901118419740425e-03 * daysPerYear,
		vz:   -6.90460016972063023e-05 * daysPerYear,
		mass: 9.54791938424326609e-04 * solarMass,
	}
	// Saturn
	bodies[2] = Body{
		x:    8.34336671824457987e+00,
		y:    4.12479856412430479e+00,
		z:    -4.03523417114321381e-01,
		vx:   -2.76742510726862411e-03 * daysPerYear,
		vy:   4.99852801234917238e-03 * daysPerYear,
		vz:   2.30417297573763929e-05 * daysPerYear,
		mass: 2.85885980666130812e-04 * solarMass,
	}
	// Uranus
	bodies[3] = Body{
		x:    1.28943695621391310e+01,
		y:    -1.51111514016986312e+01,
		z:    -2.23307578892655734e-01,
		vx:   2.96460137564761618e-03 * daysPerYear,
		vy:   2.37847173959480950e-03 * daysPerYear,
		vz:   -2.96589568540237556e-05 * daysPerYear,
		mass: 4.36624404335156298e-05 * solarMass,
	}
	// Neptune
	bodies[4] = Body{
		x:    1.53796971148509165e+01,
		y:    -2.59193146099879641e+01,
		z:    1.79258772950371181e-01,
		vx:   2.68067772490389322e-03 * daysPerYear,
		vy:   1.62824170038242295e-03 * daysPerYear,
		vz:   -9.51592254519715870e-05 * daysPerYear,
		mass: 5.15138902046611451e-05 * solarMass,
	}
}

func offsetMomentum() {
	px := 0.0
	py := 0.0
	pz := 0.0
	for i := 0; i < 5; i++ {
		px += bodies[i].vx * bodies[i].mass
		py += bodies[i].vy * bodies[i].mass
		pz += bodies[i].vz * bodies[i].mass
	}
	bodies[0].vx = -px / solarMass
	bodies[0].vy = -py / solarMass
	bodies[0].vz = -pz / solarMass
}

func energy() float64 {
	e := 0.0
	for i := 0; i < 5; i++ {
		b := &bodies[i]
		e += 0.5 * b.mass * (b.vx*b.vx + b.vy*b.vy + b.vz*b.vz)
		for j := i + 1; j < 5; j++ {
			b2 := &bodies[j]
			dx := b.x - b2.x
			dy := b.y - b2.y
			dz := b.z - b2.z
			distance := math.Sqrt(dx*dx + dy*dy + dz*dz)
			e -= (b.mass * b2.mass) / distance
		}
	}
	return e
}

func advance(dt float64) {
	for i := 0; i < 5; i++ {
		bi := &bodies[i]
		for j := i + 1; j < 5; j++ {
			bj := &bodies[j]
			dx := bi.x - bj.x
			dy := bi.y - bj.y
			dz := bi.z - bj.z
			dSquared := dx*dx + dy*dy + dz*dz
			distance := math.Sqrt(dSquared)
			mag := dt / (dSquared * distance)
			bi.vx -= dx * bj.mass * mag
			bi.vy -= dy * bj.mass * mag
			bi.vz -= dz * bj.mass * mag
			bj.vx += dx * bi.mass * mag
			bj.vy += dy * bi.mass * mag
			bj.vz += dz * bi.mass * mag
		}
	}
	for i := 0; i < 5; i++ {
		b := &bodies[i]
		b.x += dt * b.vx
		b.y += dt * b.vy
		b.z += dt * b.vz
	}
}

func main() {
	initBodies()
	offsetMomentum()
	fmt.Printf("%d\n", int64(energy()*1e9))
	for i := 0; i < Steps; i++ {
		advance(0.01)
	}
	fmt.Printf("%d\n", int64(energy()*1e9))
}
