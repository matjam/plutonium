// spectralnorm: Benchmarks Game spectral-norm (power method on an infinite
// matrix). Measures float64 division/multiply-add in tight nested loops and
// array passing. Prints int64(result * 1e9).
#include <math.h>
#include <stdint.h>
#include <stdio.h>

#define N 5500

static double u[N];
static double v[N];
static double tmp[N];

static double evalA(int64_t i, int64_t j) {
    return 1.0 / (double)((i + j) * (i + j + 1) / 2 + i + 1);
}

static void multiplyAv(int64_t n, const double *v, double *av) {
    for (int64_t i = 0; i < n; i++) {
        double sum = 0.0;
        for (int64_t j = 0; j < n; j++) {
            sum += evalA(i, j) * v[j];
        }
        av[i] = sum;
    }
}

static void multiplyAtv(int64_t n, const double *v, double *atv) {
    for (int64_t i = 0; i < n; i++) {
        double sum = 0.0;
        for (int64_t j = 0; j < n; j++) {
            sum += evalA(j, i) * v[j];
        }
        atv[i] = sum;
    }
}

static void multiplyAtAv(int64_t n, const double *v, double *tmp, double *atav) {
    multiplyAv(n, v, tmp);
    multiplyAtv(n, tmp, atav);
}

int main(void) {
    for (int i = 0; i < N; i++) {
        u[i] = 1.0;
    }
    for (int i = 0; i < 10; i++) {
        multiplyAtAv(N, u, tmp, v);
        multiplyAtAv(N, v, tmp, u);
    }
    double vBv = 0.0;
    double vv = 0.0;
    for (int i = 0; i < N; i++) {
        vBv += u[i] * v[i];
        vv += v[i] * v[i];
    }
    printf("%lld\n", (long long)(int64_t)(sqrt(vBv / vv) * 1e9));
    return 0;
}
