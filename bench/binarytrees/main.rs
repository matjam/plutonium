// binarytrees: Benchmarks Game binary-trees. Allocates and walks many short-
// lived perfect binary trees plus one long-lived tree.
// Measures allocation/deallocation throughput and pointer chasing.
//
// Memory management differs by language and is part of what is measured:
// Go relies on its garbage collector, C calls malloc/free, Rust drops Box
// values, Plutonium frees through its compiler-inserted ownership scheme.

const MAX_DEPTH: i64 = 18;

struct Node {
    left: Option<Box<Node>>,
    right: Option<Box<Node>>,
}

fn bottom_up_tree(depth: i64) -> Box<Node> {
    let mut n = Box::new(Node { left: None, right: None });
    if depth > 0 {
        n.left = Some(bottom_up_tree(depth - 1));
        n.right = Some(bottom_up_tree(depth - 1));
    }
    n
}

fn item_check(n: &Node) -> i64 {
    match (&n.left, &n.right) {
        (Some(l), Some(r)) => 1 + item_check(l) + item_check(r),
        _ => 1,
    }
}

fn stretch(depth: i64) -> i64 {
    let t = bottom_up_tree(depth);
    item_check(&t)
}

fn main() {
    let min_depth: i64 = 4;
    let mut max_depth: i64 = MAX_DEPTH;
    if min_depth + 2 > max_depth {
        max_depth = min_depth + 2;
    }

    println!("{} {}", max_depth + 1, stretch(max_depth + 1));

    let long_lived = bottom_up_tree(max_depth);

    let mut d = min_depth;
    while d <= max_depth {
        let iterations: i64 = 1 << (max_depth - d + min_depth);
        let mut check: i64 = 0;
        for _ in 0..iterations {
            let t = bottom_up_tree(d);
            check += item_check(&t);
        }
        println!("{} {} {}", iterations, d, check);
        d += 2;
    }

    println!("{} {}", max_depth, item_check(&long_lived));
}
