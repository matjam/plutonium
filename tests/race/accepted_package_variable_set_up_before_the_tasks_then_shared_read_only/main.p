package main
type Table struct {
	name string
	rows [4]int
}

func (t *Table) init(name string) {
	t.name = name
	for i := range t.rows {
		t.rows[i] = i * i
	}
}

var table Table

func init() { table.init("squares") }

func show(t *Table, done chan int) { done <- t.rows[3] }

func main() {
	done := make(chan int)
	task show(&table, done)
	task show(&table, done)
	println(<-done + <-done)
}