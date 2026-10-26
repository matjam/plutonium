package main
type Job struct{ id int }

var job Job

func main() {
	ch := make(chan *Job, 1)
	done := make(chan bool)
	task show(ch, done)
	job.id = 1
	ch <- &job
	job.id = 2
	<-done
}

func show(ch chan *Job, done chan bool) {
	println((<-ch).id)
	done <- true
}