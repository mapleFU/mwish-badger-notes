Closer 是 `ristretto` 的 `z` 的一部分。它希望提供 128bit hash, 但是返回了两个 u64.

```go
// Closer holds the two things we need to close a goroutine and wait for it to
// finish: a chan to tell the goroutine to shut down, and a WaitGroup with
// which to wait for it to finish shutting down.
type Closer struct {
	waiting sync.WaitGroup

	ctx    context.Context
	cancel context.CancelFunc
}
```

`ctx` 和 `cancel` 处理并发，并使得系统可 cancel.

创建的时候，这里添加一组 wg:

```go
// NewCloser constructs a new Closer, with an initial count on the WaitGroup.
func NewCloser(initial int) *Closer {
	ret := &Closer{}
	ret.ctx, ret.cancel = context.WithCancel(context.Background())
	ret.waiting.Add(initial)
	return ret
}
```

`Closer` 提供了两组函数：

1. Signal 系
2. Wait 系

关闭的时候，可以 `SignalAndWait`:

```go
// SignalAndWait calls Signal(), then Wait().
func (lc *Closer) SignalAndWait() {
	lc.Signal()
	lc.Wait()
}
```

Signal 和 `HasBeenClosed` 连用：

```go
// Signal signals the HasBeenClosed signal.
func (lc *Closer) Signal() {
	// Todo(ibrahim): Change Signal to return error on next badger breaking change.
	lc.cancel()
}

// HasBeenClosed gets signaled when Signal() is called.
func (lc *Closer) HasBeenClosed() <-chan struct{} {
	if lc == nil {
		return dummyCloserChan
	}
	return lc.ctx.Done()
}
```

然后 `Done` 又是另一套逻辑：

```go
// Wait waits on the WaitGroup. (It waits for NewCloser's initial value, AddRunning, and Done
// calls to balance out.)
func (lc *Closer) Wait() {
	lc.waiting.Wait()
}
```

操，什么玩意啊。