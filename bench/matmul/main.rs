// matmul: naive i-j-k triple-loop float64 matrix multiply of two NxN
// row-major matrices. The inner loop is a dot product with a strided read of
// B, so it measures float add latency, bounds-check hoisting and cache misses.
// Prints the sum of C and one element, each as int64(x * 1000).

const N: usize = 1200;

fn main() {
    let n = N;
    let mut a = vec![0.0f64; n * n];
    let mut b = vec![0.0f64; n * n];
    let mut c = vec![0.0f64; n * n];
    for i in 0..n {
        for j in 0..n {
            a[i * n + j] = ((i * 31 + j * 17) % 97) as f64 / 97.0;
            b[i * n + j] = ((i * 13 + j * 7) % 89) as f64 / 89.0;
        }
    }
    for i in 0..n {
        for j in 0..n {
            let mut sum = 0.0;
            for k in 0..n {
                sum += a[i * n + k] * b[k * n + j];
            }
            c[i * n + j] = sum;
        }
    }
    let mut total = 0.0;
    for i in 0..n * n {
        total += c[i];
    }
    println!("{}", (total * 1000.0) as i64);
    println!("{}", (c[(n / 2) * n + n / 3] * 1000.0) as i64);
}
