// C half of the System V interoperability test (see main.p). Each function
// folds every field of every argument into its result with distinct
// weights, so an argument in the wrong place changes the answer.

#include <stdbool.h>
#include <stdint.h>

// ---------------------------------------------------------------------------
// Scalars

int64_t c_scalars(int8_t a, uint8_t b, int16_t c, uint16_t d, int32_t e, uint32_t f,
		  int64_t g, uint64_t h, bool z) {
	return (int64_t)a + (int64_t)b * 3 + (int64_t)c * 5 + (int64_t)d * 7 + (int64_t)e * 11 +
	       (int64_t)f * 13 + g * 17 + (int64_t)h * 19 + (z ? 23 : 0);
}

// Narrow results: GCC leaves the upper bits of the register unspecified.
uint8_t c_ret_u8(uint8_t x) { return x + 100; }
int8_t c_ret_i8(int8_t x) { return x - 100; }
uint16_t c_ret_u16(uint16_t x) { return x * 3; }
int16_t c_ret_i16(int16_t x) { return -x; }
bool c_ret_bool(int32_t x) { return x > 3; }

double c_floats(float a, double b, float c, double d) { return a + b * 2 + c * 4 + d * 8; }
float c_ret_f32(float a, float b) { return a * b; }
double c_mixed(int32_t a, double b, int64_t c, float d) { return a + b * 3 + (double)c * 5 + d * 7; }

// ---------------------------------------------------------------------------
// Structs: INTEGER only

struct I1 { int32_t a; };
struct I2 { int32_t a, b; };
struct I3 { int64_t a; int8_t b; int16_t c; };
struct I4 { int16_t a, b, c, d; };

int64_t c_i1(struct I1 s) { return s.a * 3; }
int64_t c_i2(struct I2 s) { return s.a * 3 + s.b * 5; }
int64_t c_i3(struct I3 s) { return s.a * 3 + s.b * 5 + s.c * 7; }
int64_t c_i4(struct I4 s) { return s.a * 3 + s.b * 5 + s.c * 7 + s.d * 11; }

struct I1 c_make_i1(int32_t a) { return (struct I1){a + 1}; }
struct I2 c_make_i2(int32_t a, int32_t b) { return (struct I2){a + 1, b + 2}; }
struct I3 c_make_i3(int64_t a, int8_t b, int16_t c) { return (struct I3){a + 1, b + 2, c + 3}; }
struct I4 c_make_i4(int16_t a) { return (struct I4){a, a + 1, a + 2, a + 3}; }

// ---------------------------------------------------------------------------
// Structs: SSE only

struct F1 { float a; };
struct F2 { float a, b; };
struct F3 { float a, b, c; };
struct F4 { float a, b, c, d; };
struct D2 { double a, b; };

double c_f1(struct F1 s) { return s.a * 3; }
double c_f2(struct F2 s) { return s.a * 3 + s.b * 5; }
double c_f3(struct F3 s) { return s.a * 3 + s.b * 5 + s.c * 7; }
double c_f4(struct F4 s) { return s.a * 3 + s.b * 5 + s.c * 7 + s.d * 11; }
double c_d2(struct D2 s) { return s.a * 3 + s.b * 5; }

struct F1 c_make_f1(float a) { return (struct F1){a * 2}; }
struct F2 c_make_f2(float a, float b) { return (struct F2){a * 2, b * 2}; }
struct F3 c_make_f3(float a) { return (struct F3){a, a + 1, a + 2}; }
struct F4 c_make_f4(float a) { return (struct F4){a, a + 1, a + 2, a + 3}; }
struct D2 c_make_d2(double a, double b) { return (struct D2){a + b, a - b}; }

// ---------------------------------------------------------------------------
// Structs: mixed classes

struct M2 { int64_t a; double b; };		// INTEGER, SSE
struct M2b { double a; int32_t b; };		// SSE, INTEGER
struct M3 { float a; int32_t b; double c; };	// INTEGER (float + int share), SSE
struct M4 { int8_t a; uint8_t b; float c; double d; };	// INTEGER, SSE

double c_m2(struct M2 s) { return s.a * 3 + s.b * 5; }
double c_m2b(struct M2b s) { return s.a * 3 + s.b * 5; }
double c_m3(struct M3 s) { return s.a * 3 + s.b * 5 + s.c * 7; }
double c_m4(struct M4 s) { return s.a * 3 + s.b * 5 + s.c * 7 + s.d * 11; }

struct M2 c_make_m2(int64_t a, double b) { return (struct M2){a * 2, b * 2}; }
struct M2b c_make_m2b(double a, int32_t b) { return (struct M2b){a * 2, b * 2}; }
struct M3 c_make_m3(float a, int32_t b, double c) { return (struct M3){a + 1, b + 1, c + 1}; }
struct M4 c_make_m4(int8_t a) { return (struct M4){a, (uint8_t)(a + 1), a + 2.5f, a + 3.5}; }

// ---------------------------------------------------------------------------
// Memory-class structs

struct Big { int64_t a; double b; int32_t c; };	// 24 bytes
struct Huge { int64_t v[5]; };			// 40 bytes

double c_big(struct Big s, int64_t k) { return s.a * 3 + s.b * 5 + s.c * 7 + k * 11; }
struct Big c_make_big(int64_t a, double b, int32_t c) { return (struct Big){a + 1, b + 1, c + 1}; }
struct Big c_big_round_trip(struct Big s) { return (struct Big){s.a * 2, s.b * 2, s.c * 2}; }
int64_t c_huge(struct Huge h, struct Huge g) {
	int64_t sum = 0;
	for (int i = 0; i < 5; i++)
		sum += h.v[i] * (i + 1) - g.v[i] * (i + 7);
	return sum;
}

// ---------------------------------------------------------------------------
// Running out of registers

int64_t c_many(int64_t a, int64_t b, int64_t c, int64_t d, int64_t e, int64_t f, int64_t g,
	       int64_t h, double x0, double x1, double x2, double x3, double x4, double x5,
	       double x6, double x7, double x8, double x9, int8_t n, float y) {
	return a + b * 2 + c * 3 + d * 4 + e * 5 + f * 6 + g * 7 + h * 8 +
	       (int64_t)(x0 * 9 + x1 * 10 + x2 * 11 + x3 * 12 + x4 * 13 + x5 * 14 + x6 * 15 + x7 * 16 +
			 x8 * 17 + x9 * 18) +
	       n * 19 + (int64_t)(y * 20);
}

// I3 needs two integer registers but only R9 is left: it goes to the stack
// whole, and the int64 after it still takes R9.
int64_t c_spill_int(int64_t a, int64_t b, int64_t c, int64_t d, int64_t e, struct I3 s, int64_t f) {
	return a + b * 2 + c * 3 + d * 4 + e * 5 + s.a * 6 + s.b * 7 + s.c * 8 + f * 9;
}

// D2 needs two SSE registers but only XMM7 is left.
double c_spill_sse(double a, double b, double c, double d, double e, double f, double g,
		   struct D2 s, double h) {
	return a + b * 2 + c * 3 + d * 4 + e * 5 + f * 6 + g * 7 + s.a * 8 + s.b * 9 + h * 10;
}

// A memory struct with a hidden result pointer taking RDI.
struct Big c_sret_many(int64_t a, int64_t b, int64_t c, int64_t d, int64_t e, int64_t f) {
	return (struct Big){a + b + c + d + e + f, (double)(a * f), (int32_t)(b - e)};
}

// ---------------------------------------------------------------------------
// Several Plutonium results are a struct of the results.

struct DivMod { int64_t q, r; };
struct DivMod c_divmod(int64_t a, int64_t b) { return (struct DivMod){a / b, a % b}; }

struct Pair32 { int32_t a, b; };	// one eightbyte in RAX
struct Pair32 c_pair32(int32_t x) { return (struct Pair32){x + 1, x + 2}; }

struct Three { int64_t a, b, c; };	// memory
struct Three c_three(int64_t x) { return (struct Three){x, x * 2, x * 3}; }

struct IntDouble { int64_t n; double d; };
struct IntDouble c_int_double(int64_t x) { return (struct IntDouble){x * 3, x * 0.5}; }

// ---------------------------------------------------------------------------
// Packed and volatile types cross as their storage.

struct WithHeader { uint8_t hdr[6]; int16_t x; };	// one INTEGER eightbyte
struct VolMix { int32_t v; float f; };			// INTEGER (int and float share)

int64_t c_ctl(uint32_t ctl) { return ctl & 0xFFF; }
int64_t c_with_header(struct WithHeader w) {
	return w.hdr[0] + w.hdr[1] * 10 + (w.hdr[2] | w.hdr[3] << 8 | w.hdr[4] << 16 | w.hdr[5] << 24) * 100 + w.x * 7;
}
struct VolMix c_vol_mix(struct VolMix v) { return (struct VolMix){v.v * 2, v.f * 2}; }

// ---------------------------------------------------------------------------
// Callbacks into Plutonium through //pu:export

struct Big pu_scale_big(struct Big b, int64_t k);
struct M2b pu_mixed(struct M2 m, float f, int8_t i);
struct DivMod pu_divmod(int64_t a, int64_t b);
struct Three pu_three(int64_t x);
uint8_t pu_narrow(int8_t a, uint16_t b);
uint32_t pu_narrow_wide(int8_t a, uint16_t b) __asm__("pu_narrow");
bool pu_is_big(int64_t x);
double pu_spill(int64_t a, int64_t b, int64_t c, int64_t d, int64_t e, struct I3 s, int64_t f,
		double x0, double x1, double x2, double x3, double x4, double x5, double x6,
		struct D2 t, double x7);
struct F3 pu_f3(struct F3 v);
struct I3 pu_point(int64_t a, int8_t b, int16_t c);
int64_t pu_huge(struct Huge h);
struct M2 pu_m2_scale(struct M2 m, double k);

// c_callbacks calls every exported function and returns a bitmask of the
// checks that failed: 0 when all pass.
int64_t c_callbacks(void) {
	int64_t fail = 0;
	struct Big b = pu_scale_big((struct Big){3, 1.5, -4}, 10);
	if (b.a != 30 || b.b != 15.0 || b.c != -40)
		fail |= 1 << 0;
	struct M2b m = pu_mixed((struct M2){7, 2.25}, 0.5f, -3);
	if (m.a != 7 + 2.25 + 0.5 || m.b != -3 * 7)
		fail |= 1 << 1;
	struct DivMod dm = pu_divmod(47, 5);
	if (dm.q != 9 || dm.r != 2)
		fail |= 1 << 2;
	struct Three t = pu_three(4);
	if (t.a != 5 || t.b != 6 || t.c != 7)
		fail |= 1 << 3;
	// A narrow result arrives widened to 32 bits, as Clang callers assume:
	// read the whole of EAX through a second declaration of pu_narrow.
	if (pu_narrow_wide(-2, 300) != (uint8_t)(-2 + 300))
		fail |= 1 << 4;
	if (pu_narrow(5, 7) != 12)
		fail |= 1 << 5;
	if (!pu_is_big(1000) || pu_is_big(3))
		fail |= 1 << 6;
	double s = pu_spill(1, 2, 3, 4, 5, (struct I3){6, 7, 8}, 9, 1, 2, 3, 4, 5, 6, 7,
			    (struct D2){8, 9}, 10);
	if (s != 1 + 2 + 3 + 4 + 5 + 6 + 7 + 8 + 9 + (1 + 2 + 3 + 4 + 5 + 6 + 7 + 8 + 9 + 10) * 100.0)
		fail |= 1 << 7;
	struct F3 f = pu_f3((struct F3){1, 2, 3});
	if (f.a != 3 || f.b != 2 || f.c != 1)
		fail |= 1 << 8;
	struct I3 p = pu_point(-5, 6, -7);
	if (p.a != -5 || p.b != 6 || p.c != -7)
		fail |= 1 << 9;
	if (pu_huge((struct Huge){{1, 2, 3, 4, 5}}) != 1 + 20 + 300 + 4000 + 50000)
		fail |= 1 << 10;
	// An exported method: the receiver is the first parameter.
	struct M2 sm = pu_m2_scale((struct M2){-3, 0.25}, 4);
	if (sm.a != -12 || sm.b != 1)
		fail |= 1 << 11;
	return fail;
}

// c_apply calls a C function pointer, which here is a Plutonium export.
int64_t c_apply(int64_t (*fn)(struct I2), int32_t a, int32_t b) { return fn((struct I2){a, b}); }

// c_address_of returns the address of the exported pu_i2_sum, for c_apply.
int64_t pu_i2_sum(struct I2 s);
void *c_address_of_sum(void) { return (void *)pu_i2_sum; }
