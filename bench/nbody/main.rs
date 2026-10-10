// nbody: Benchmarks Game n-body (Jovian planets, symplectic integrator).
// Measures float64 arithmetic, sqrt, and field access on an array of structs.
//
// Prints the system energy before and after the run as int64(e * 1e9).

const STEPS: i64 = 35000000;

#[derive(Clone, Copy)]
struct Body {
    x: f64,
    y: f64,
    z: f64,
    vx: f64,
    vy: f64,
    vz: f64,
    mass: f64,
}

fn init_bodies(pi: f64, days_per_year: f64) -> ([Body; 5], f64) {
    let solar_mass = 4.0 * pi * pi;
    let bodies = [
        // Sun
        Body { x: 0.0, y: 0.0, z: 0.0, vx: 0.0, vy: 0.0, vz: 0.0, mass: solar_mass },
        // Jupiter
        Body {
            x: 4.84143144246472090e+00,
            y: -1.16032004402742839e+00,
            z: -1.03622044471123109e-01,
            vx: 1.66007664274403694e-03 * days_per_year,
            vy: 7.69901118419740425e-03 * days_per_year,
            vz: -6.90460016972063023e-05 * days_per_year,
            mass: 9.54791938424326609e-04 * solar_mass,
        },
        // Saturn
        Body {
            x: 8.34336671824457987e+00,
            y: 4.12479856412430479e+00,
            z: -4.03523417114321381e-01,
            vx: -2.76742510726862411e-03 * days_per_year,
            vy: 4.99852801234917238e-03 * days_per_year,
            vz: 2.30417297573763929e-05 * days_per_year,
            mass: 2.85885980666130812e-04 * solar_mass,
        },
        // Uranus
        Body {
            x: 1.28943695621391310e+01,
            y: -1.51111514016986312e+01,
            z: -2.23307578892655734e-01,
            vx: 2.96460137564761618e-03 * days_per_year,
            vy: 2.37847173959480950e-03 * days_per_year,
            vz: -2.96589568540237556e-05 * days_per_year,
            mass: 4.36624404335156298e-05 * solar_mass,
        },
        // Neptune
        Body {
            x: 1.53796971148509165e+01,
            y: -2.59193146099879641e+01,
            z: 1.79258772950371181e-01,
            vx: 2.68067772490389322e-03 * days_per_year,
            vy: 1.62824170038242295e-03 * days_per_year,
            vz: -9.51592254519715870e-05 * days_per_year,
            mass: 5.15138902046611451e-05 * solar_mass,
        },
    ];
    (bodies, solar_mass)
}

fn offset_momentum(bodies: &mut [Body; 5], solar_mass: f64) {
    let mut px = 0.0;
    let mut py = 0.0;
    let mut pz = 0.0;
    for b in bodies.iter() {
        px += b.vx * b.mass;
        py += b.vy * b.mass;
        pz += b.vz * b.mass;
    }
    bodies[0].vx = -px / solar_mass;
    bodies[0].vy = -py / solar_mass;
    bodies[0].vz = -pz / solar_mass;
}

fn energy(bodies: &[Body; 5]) -> f64 {
    let mut e = 0.0;
    for i in 0..5 {
        let b = &bodies[i];
        e += 0.5 * b.mass * (b.vx * b.vx + b.vy * b.vy + b.vz * b.vz);
        for j in (i + 1)..5 {
            let b2 = &bodies[j];
            let dx = b.x - b2.x;
            let dy = b.y - b2.y;
            let dz = b.z - b2.z;
            let distance = (dx * dx + dy * dy + dz * dz).sqrt();
            e -= (b.mass * b2.mass) / distance;
        }
    }
    e
}

fn advance(bodies: &mut [Body; 5], dt: f64) {
    for i in 0..5 {
        let (head, tail) = bodies.split_at_mut(i + 1);
        let bi = &mut head[i];
        for bj in tail.iter_mut() {
            let dx = bi.x - bj.x;
            let dy = bi.y - bj.y;
            let dz = bi.z - bj.z;
            let d_squared = dx * dx + dy * dy + dz * dz;
            let distance = d_squared.sqrt();
            let mag = dt / (d_squared * distance);
            bi.vx -= dx * bj.mass * mag;
            bi.vy -= dy * bj.mass * mag;
            bi.vz -= dz * bj.mass * mag;
            bj.vx += dx * bi.mass * mag;
            bj.vy += dy * bi.mass * mag;
            bj.vz += dz * bi.mass * mag;
        }
    }
    for b in bodies.iter_mut() {
        b.x += dt * b.vx;
        b.y += dt * b.vy;
        b.z += dt * b.vz;
    }
}

fn main() {
    let (mut bodies, solar_mass) = init_bodies(3.141592653589793, 365.24);
    offset_momentum(&mut bodies, solar_mass);
    println!("{}", (energy(&bodies) * 1e9) as i64);
    for _ in 0..STEPS {
        advance(&mut bodies, 0.01);
    }
    println!("{}", (energy(&bodies) * 1e9) as i64);
}
