// structs: particle update loop built from small 3D-vector structs that are
// passed and returned by value (by-value methods, struct-returning functions,
// whole-struct element copies). Each particle is a charged mass on a spring in
// a uniform magnetic field, integrated with semi-implicit Euler. Measures
// struct passing/return ABI, inlining, and scalar replacement of aggregates.
// Prints kinetic and potential sums and one coordinate.

const NP: usize = 1000;
const STEPS: usize = 500000;

#[derive(Clone, Copy)]
struct Vec3 {
    x: f64,
    y: f64,
    z: f64,
}

impl Vec3 {
    fn new(x: f64, y: f64, z: f64) -> Vec3 {
        Vec3 { x, y, z }
    }

    fn add(self, b: Vec3) -> Vec3 {
        Vec3::new(self.x + b.x, self.y + b.y, self.z + b.z)
    }

    fn scale(self, s: f64) -> Vec3 {
        Vec3::new(self.x * s, self.y * s, self.z * s)
    }

    fn dot(self, b: Vec3) -> f64 {
        self.x * b.x + self.y * b.y + self.z * b.z
    }

    fn cross(self, b: Vec3) -> Vec3 {
        Vec3::new(
            self.y * b.z - self.z * b.y,
            self.z * b.x - self.x * b.z,
            self.x * b.y - self.y * b.x,
        )
    }
}

#[derive(Clone, Copy)]
struct Particle {
    pos: Vec3,
    vel: Vec3,
    inv_mass: f64,
}

fn force(p: Particle, field: Vec3, k: f64) -> Vec3 {
    let spring = p.pos.scale(-k);
    let lorentz = p.vel.cross(field);
    spring.add(lorentz).scale(p.inv_mass)
}

fn step(mut p: Particle, field: Vec3, k: f64, dt: f64) -> Particle {
    let acc = force(p, field, k);
    p.vel = p.vel.add(acc.scale(dt));
    p.pos = p.pos.add(p.vel.scale(dt));
    p
}

fn main() {
    let mut particles: Vec<Particle> = Vec::with_capacity(NP);
    for i in 0..NP {
        particles.push(Particle {
            pos: Vec3::new((i % 17) as f64 - 8.0, (i % 13) as f64 - 6.0, (i % 11) as f64 - 5.0),
            vel: Vec3::new(
                ((i % 7) as f64 - 3.0) * 0.1,
                ((i % 5) as f64 - 2.0) * 0.1,
                ((i % 3) as f64 - 1.0) * 0.1,
            ),
            inv_mass: 1.0 / (1 + i % 5) as f64,
        });
    }
    let field = Vec3::new(0.0, 0.0, 0.5);
    let k = 1.0;
    let dt = 0.001;
    for _ in 0..STEPS {
        for i in 0..NP {
            particles[i] = step(particles[i], field, k, dt);
        }
    }
    let mut kinetic = 0.0;
    let mut potential = 0.0;
    for i in 0..NP {
        kinetic += particles[i].vel.dot(particles[i].vel);
        potential += particles[i].pos.dot(particles[i].pos);
    }
    println!("{}", (kinetic * 1e6) as i64);
    println!("{}", (potential * 1e6) as i64);
    println!("{}", (particles[NP / 2].pos.x * 1e9) as i64);
}
