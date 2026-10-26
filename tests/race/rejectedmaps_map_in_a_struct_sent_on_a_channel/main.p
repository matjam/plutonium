package main
type job struct {
	results map[string]int
}

func work(jobs chan job, done chan bool) {
	j := <-jobs
	j.results["n"] = 1
	done <- true
}

func main() {
	jobs := make(chan job, 2)
	done := make(chan bool)
	r := map[string]int{}
	task work(jobs, done)
	task work(jobs, done)
	jobs <- job{results: r}
	jobs <- job{results: r}
	<-done
	<-done
}