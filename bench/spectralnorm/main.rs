// spectralnorm: Benchmarks Game spectral-norm (power method on an infinite
// matrix). Measures float64 division/multiply-add in tight nested loops and
// slice passing. Prints int64(result * 1e9).

const N: usize = 5500;

fn eval_a(i: i64, j: i64) -> f64 {
    1.0 / ((i + j) * (i + j + 1) / 2 + i + 1) as f64
}

fn multiply_av(v: &[f64], av: &mut [f64]) {
    let n = v.len() as i64;
    for i in 0..n {
        let mut sum = 0.0;
        for j in 0..n {
            sum += eval_a(i, j) * v[j as usize];
        }
        av[i as usize] = sum;
    }
}

fn multiply_atv(v: &[f64], atv: &mut [f64]) {
    let n = v.len() as i64;
    for i in 0..n {
        let mut sum = 0.0;
        for j in 0..n {
            sum += eval_a(j, i) * v[j as usize];
        }
        atv[i as usize] = sum;
    }
}

fn multiply_atav(v: &[f64], tmp: &mut [f64], atav: &mut [f64]) {
    multiply_av(v, tmp);
    multiply_atv(tmp, atav);
}

fn main() {
    let mut u = vec![1.0f64; N];
    let mut v = vec![0.0f64; N];
    let mut tmp = vec![0.0f64; N];
    for _ in 0..10 {
        multiply_atav(&u, &mut tmp, &mut v);
        multiply_atav(&v, &mut tmp, &mut u);
    }
    let mut vbv = 0.0;
    let mut vv = 0.0;
    for i in 0..N {
        vbv += u[i] * v[i];
        vv += v[i] * v[i];
    }
    println!("{}", ((vbv / vv).sqrt() * 1e9) as i64);
}
