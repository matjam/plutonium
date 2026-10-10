// matmul: naive i-j-k triple-loop float64 matrix multiply of two NxN
// row-major matrices. The inner loop is a dot product with a strided read of
// B, so it measures float add latency, bounds-check hoisting and cache misses.
// Prints the sum of C and one element, each as int64(x * 1000).
#include <stdint.h>
#include <stdio.h>

#define N 1200

static double a[N * N];
static double b[N * N];
static double c[N * N];

int main(void) {
    int64_t n = N;
    for (int64_t i = 0; i < n; i++) {
        for (int64_t j = 0; j < n; j++) {
            a[i * n + j] = (double)((i * 31 + j * 17) % 97) / 97.0;
            b[i * n + j] = (double)((i * 13 + j * 7) % 89) / 89.0;
        }
    }
    for (int64_t i = 0; i < n; i++) {
        for (int64_t j = 0; j < n; j++) {
            double sum = 0.0;
            for (int64_t k = 0; k < n; k++) {
                sum += a[i * n + k] * b[k * n + j];
            }
            c[i * n + j] = sum;
        }
    }
    double total = 0.0;
    for (int64_t i = 0; i < n * n; i++) {
        total += c[i];
    }
    printf("%lld\n", (long long)(int64_t)(total * 1000));
    printf("%lld\n", (long long)(int64_t)(c[(n / 2) * n + n / 3] * 1000));
    return 0;
}
