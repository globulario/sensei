// Positive-control fixture for failure_amplify_fire_and_forget_goroutine.
// go func(){...}() inside an if err != nil block.
package badfix

func handle(connect func() error, recover2 func()) {
	err := connect()
	if err != nil {
		go func() { // BAD: unbounded fire-and-forget on error path
			recover2()
		}()
	}
}
