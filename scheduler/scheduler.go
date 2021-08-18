package scheduler

import "time"

var schedule = &Schedule{}

func init() {
	go schedule.populate()

	for range time.Tick(time.Millisecond * 200) {

	}

}
