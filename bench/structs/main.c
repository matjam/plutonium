// structs: particle update loop built from small 3D-vector structs that are
// passed and returned by value (struct-returning functions, whole-struct array
// element copies). Each particle is a charged mass on a spring in a uniform
// magnetic field, integrated with semi-implicit Euler. Measures struct
// passing/return ABI, inlining, and scalar replacement of aggregates.
// Prints kinetic and potential sums and one coordinate.
#include <stdint.h>
#include <stdio.h>

#define NP 1000
#define STEPS 500000

typedef struct {
    double X, Y, Z;
} Vec3;

static Vec3 vadd(Vec3 a, Vec3 b) {
    return (Vec3){a.X + b.X, a.Y + b.Y, a.Z + b.Z};
}

static Vec3 vscale(Vec3 a, double s) {
    return (Vec3){a.X * s, a.Y * s, a.Z * s};
}

static double vdot(Vec3 a, Vec3 b) {
    return a.X * b.X + a.Y * b.Y + a.Z * b.Z;
}

static Vec3 vcross(Vec3 a, Vec3 b) {
    return (Vec3){a.Y * b.Z - a.Z * b.Y, a.Z * b.X - a.X * b.Z, a.X * b.Y - a.Y * b.X};
}

typedef struct {
    Vec3 Pos;
    Vec3 Vel;
    double InvMass;
} Particle;

static Particle particles[NP];

static Vec3 force(Particle p, Vec3 field, double k) {
    Vec3 spring = vscale(p.Pos, -k);
    Vec3 lorentz = vcross(p.Vel, field);
    return vscale(vadd(spring, lorentz), p.InvMass);
}

static Particle step(Particle p, Vec3 field, double k, double dt) {
    Vec3 acc = force(p, field, k);
    p.Vel = vadd(p.Vel, vscale(acc, dt));
    p.Pos = vadd(p.Pos, vscale(p.Vel, dt));
    return p;
}

int main(void) {
    for (int i = 0; i < NP; i++) {
        particles[i] = (Particle){
            .Pos = {(double)(i % 17) - 8, (double)(i % 13) - 6, (double)(i % 11) - 5},
            .Vel = {((double)(i % 7) - 3) * 0.1, ((double)(i % 5) - 2) * 0.1, ((double)(i % 3) - 1) * 0.1},
            .InvMass = 1.0 / (double)(1 + i % 5),
        };
    }
    Vec3 field = {0.0, 0.0, 0.5};
    double k = 1.0;
    double dt = 0.001;
    for (int s = 0; s < STEPS; s++) {
        for (int i = 0; i < NP; i++) {
            particles[i] = step(particles[i], field, k, dt);
        }
    }
    double kinetic = 0.0;
    double potential = 0.0;
    for (int i = 0; i < NP; i++) {
        kinetic += vdot(particles[i].Vel, particles[i].Vel);
        potential += vdot(particles[i].Pos, particles[i].Pos);
    }
    printf("%lld\n", (long long)(int64_t)(kinetic * 1e6));
    printf("%lld\n", (long long)(int64_t)(potential * 1e6));
    printf("%lld\n", (long long)(int64_t)(particles[NP / 2].Pos.X * 1e9));
    return 0;
}
