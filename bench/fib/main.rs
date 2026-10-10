// fib: naive doubly-recursive Fibonacci. Measures call/return overhead.

const N: i64 = 43;

fn fib(n: i64) -> i64 {
    if n < 2 {
        return n;
    }
    fib(n - 1) + fib(n - 2)
}

fn main() {
    println!("{}", fib(N));
}
