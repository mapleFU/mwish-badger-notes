Badger 有一个 managed mode, 是否是 managed mode, 标志是否由外部管理 txn 的 ts 等。

在这节，我们关注 badger 简单的读写流程和相关的对象。Badger 进行了很多很神奇的封装，有的时候，我认为它们是冗余的，但是它们对理解 Badger 内容是必要的。

读链路上，除了 `open()` `close`, 两个比较重要的方法如下：

```go
func (db *DB) View(fn func(txn *Txn) error) error
func (db *DB) Update(fn func(txn *Txn) error) error
```

这两个内部流程大概是：

```go
	txn := db.NewTransaction(/* 是否 update */)
	defer txn.Discard()

	if err := fn(txn); err != nil {
		return err
	}

	return txn.Commit()
```

然后进入事务流程

## 事务和水位

事务的结构体如下（这个只涉及内部实现，不太对外）：

```go

// Txn represents a Badger transaction.
type Txn struct {
  // 读写事务都有的读时间戳
	readTs   uint64
  // 只有写事务才有的时间戳
	commitTs uint64
	// size 和 count 仅对写事务有用
  // 估计的修改的事务的大小(key + value)
	size  int64
  // 修改的事务的个数
	count int64
	// 绑定的 db 对象, 读的起源.
	db *DB

	// 这里的读取不采用 key set, 采用 fingerprint, 这里不会误判, 但是可能有 false positive.
	// 这个是为写事务准备的 read set.
	reads []uint64 // contains fingerprints of keys read.
	// contains fingerprints of keys written. This is used for conflict detection.
	conflictKeys map[uint64]struct{}
	readsLock    sync.Mutex // guards the reads slice. See addReadKey.

	pendingWrites   map[string]*Entry // cache stores any writes done by txn.
	duplicateWrites []*Entry          // Used in managed mode to store duplicate entries.

	numIterators int32
	discarded    bool
	doneRead     bool
	// 是否是更新的事务.
	update bool // update is used to conditionally keep track of reads.
}
```

这里可以看到，有一些用于鉴定冲突的字段和状态字段。

我们要从上面这个 `txn` 的创建讲起，对应的内容在 `DB.NewTransaction`. 忽略 managed mode：

```go
func (db *DB) newTransaction(update, isManaged bool) *Txn {
	if db.opt.ReadOnly && update {
		// DB is read-only, force read-only transaction.
		update = false
	}

	txn := &Txn{
		update: update,
		db:     db,
		count:  1,                       // One extra entry for BitFin.
		size:   int64(len(txnKey) + 10), // Some buffer for the extra entry.
	}
	if update {
		if db.opt.DetectConflicts {
			txn.conflictKeys = make(map[uint64]struct{})
		}
		txn.pendingWrites = make(map[string]*Entry)
	}
	if !isManaged {
		txn.readTs = db.orc.readTs()
	}
	return txn
}

```

这里会：

1. 给事务创建，然后添加 `Fin` 的边界值
2. 如果是写事务，添加 `conflictKeys` 做冲突检测，加 `pendingWrites` 做写入 buffer。
3. 拿到一个 `readTs` 作为读水位

写入的时候，`Set` 接口对应拿到的是 `key-value` pair：

```go
// Entry provides Key, Value, UserMeta and ExpiresAt. This struct can be used by
// the user to set data.
//
// Key, Value 和 TTL. meta 定义在 value.go 中, 比如删除的时候, 外部只会传入 key, meta 为
// del.
// 如果是 `Set` 的话, 就会 `NewEntry` (创建 key/value) -> `modify`(保证合法).
//
// 外部传进来的时候, 只有 Key, Value, ExpiresAt, UserMeta.
type Entry struct {
	Key       []byte
	Value     []byte
	ExpiresAt uint64 // time.Unix
	version   uint64
	offset    uint32 // offset is an internal field.
	UserMeta  byte
	meta      byte

	// Fields maintained internally.
	hlen         int // Length of the header.
	valThreshold int64
}
```

这里正常写入流程中，内容如下：

```go
&Entry{
		Key:  key,
		meta: bitDelete,
}

&Entry{
		Key:   key,
		Value: value,
}
```

我们重点关注 `Key` `Value` `ExpiresAt` `offset` `meta` 等字段。meta 内容很重要，甚至会写进 vLog:

```go
// maxVlogFileSize is the maximum size of the vlog file which can be created. Vlog Offset is of
// uint32, so limiting at max uint32.
var maxVlogFileSize uint32 = math.MaxUint32

// Values have their first byte being byteData or byteDelete. This helps us distinguish between
// a key that has never been seen and a key that has been explicitly deleted.
const (
	bitDelete                 byte = 1 << 0 // Set if the key has been deleted.
	bitValuePointer           byte = 1 << 1 // Set if the value is NOT stored directly next to key.
	BitDiscardEarlierVersions byte = 1 << 2 // Set if earlier versions can be discarded.
	// Set if item shouldn't be discarded via compactions (used by merge operator)
	bitMergeEntry byte = 1 << 3
	// The MSB 2 bits are for transactions.
	bitTxn    byte = 1 << 6 // Set if the entry is part of a txn.
	bitFinTxn byte = 1 << 7 // Set if the entry is to indicate end of txn in value log.

	mi int64 = 1 << 20

	// size of vlog header.
	// +----------------+------------------+
	// | keyID(8 bytes) |  baseIV(12 bytes)|
	// +----------------+------------------+
	vlogHeaderSize = 20
)
```

1. delete 表示是一个删除字段
2. ValuePointer 表示内容在 vLog
3. bitTxn 基本不用管，非 manage mode 的情况下，表示基本的内容写包裹在一个事务中。bitTxnFin 表示一个事务的结束，事务会 wrap 一个这样的字段进去。

然后事务写会记录在 `pendingWrites` 和 `conflictKeys` 里面，我们看看写入提交的时候，这里会发起一个 `Commit()` ，不过很有意思的是，Commit 在 `txn.Update` 的时候会自动处理 `Commit`. 这里的逻辑大概如下（代码很短，我就直接复制了）：

```go
func (txn *Txn) Commit() error {
	// txn.conflictKeys can be zero if conflict detection is turned off. So we
	// should check txn.pendingWrites.
	if len(txn.pendingWrites) == 0 {
		return nil // Nothing to do.
	}
	// Precheck before discarding txn.
	if err := txn.commitPrecheck(); err != nil {
		return err
	}
	defer txn.Discard()

	// 发送到 LSMTree 和 memtable, 来执行写.
	txnCb, err := txn.commitAndSend()
	if err != nil {
		return err
	}
	// If batchSet failed, LSM would not have been updated. So, no need to rollback anything.

	// TODO: What if some of the txns successfully make it to value log, but others fail.
	// Nothing gets updated to LSM, until a restart happens.
	return txnCb()
}
```

1. 检查是否没写入、是否有重复提交
2. `commitAndSend` 是最主要的逻辑，这里会保证写入 `writeCh` 的事务的 commit ts 是单调的。
3. 调用 `oracle.newCommitTs` , oracle 会维护: 事务冲突 + 读写水位, 然后逻辑上是线程安全的.
4. 给事务的 key 编码上版本（以 bigendian u64 的形式），然后加上 FinTxn 的事务
5. 调用 `commitAndSend`, 这里要用确定的时序发送给写入 Memtable 和 LSM 的线程.
6. 看是否成功.


我们看看 Oracle 维护的水位：

```go
type oracle struct {
	isManaged       bool // Does not change value, so no locking required.
	detectConflicts bool // Determines if the txns should be checked for conflicts.

	sync.Mutex // For nextTxnTs and commits.
	// writeChLock lock is for ensuring that transactions go to the write
	// channel in the same order as their commit timestamps.
	writeChLock sync.Mutex
	nextTxnTs   uint64

	// Used to block NewTransaction, so all previous commits are visible to a new read.
	txnMark *y.WaterMark

	// Either of these is used to determine which versions can be permanently
	// discarded during compaction.
	discardTs uint64 // Used by ManagedDB.
	// 相当于读的水位
	readMark *y.WaterMark // Used by DB.

	// committedTxns contains all committed writes (contains fingerprints
	// of keys written and their latest commit counter).
	//
	// 缓存了 committed 的写, 随着读提高而释放部分.
	committedTxns []committedTxn
	lastCleanupTs uint64

	// closer is used to stop watermarks.
	closer *z.Closer
}
```

老样子，managed mode 我们不管。重要的是：

1. `committedTxns`, 一个模糊的事务冲突表。为什么说是模糊的呢？有两点原因:
   1. 原因 1 是, 事务相关的 `committedTxns` 都是 fingerprint, 而不是完善的事务纪录(不过也没啥关系).
   2. 在 `newCommitTs` 函数调用的时候, 这里只判断了冲突, 没有管这个事务后续是 commit 了还是 abort 了, 就留在了事务表里
2. `nextTxnTs`, 这个值是和 `txnMark` 联动的. 表示提交事务的下一个 ts, 不过比较好玩的是, 这里事务也是会失败的. (TODO(mwish): 这里失败会把这个事务 nextTxnTs 恢复吗?)
3. `lastCleanupTs` 和 `readMark` 有关, 是 `committedTxns` GC 有关的, 表示上一次对 `committedTxns` GC 的时间.

那么, 虽然讲述的位置怪怪的, 但我们复习一下这两个水位是怎么维护的:

### 读和读写事务

读需要拿到一个对应的 readTs, 这里调用了 `oracle.readTs`, 这里 `readTs = o.nextTxnTs - 1`, 然后最有意思的是, `o.txnMark.WaitForMark(context.Background(), readTs)`. 这里表面上很好理解, *不带锁* 等待写事务 `o.nextTxnTs - 1` 结束嘛, 很简单. 欸但是问题来了, 这里为什么要等待呢?

答案要在读写事务寻找, 写事务会先拿到一个 `readTs`, 就走上面这段的逻辑。然后提交的时候在 `o.newCommitTs`, 这里逻辑如下:

1. 检查是否冲突, 有冲突就 abort 掉了
2. 对读事务 resolve, 这里有点 confusing, 这里终止了事务的读阶段, 然后提高了 `nextTxnTs`, 让事务去完成剩下的阶段. 我们回到读这里, `readTs` 拿到的是 "最近的一个准备写入的事务" 的 ts, 感觉极端写入场景下, 读写串行感觉不是很好？

以上就是事务的读写流程.

### 事务的提交

`Txn.commitAndSend` 维护了对 `writeCh` 的写, 并把结果的处理封装到了返回的 `txnCb` 里面. 这里 explicit 的调用了 `orc.writeCh` 来保证顺序性. 我们上面已经贴了部分流程了:

1. `commitAndSend` 是最主要的逻辑，这里会保证写入 `writeCh` 的事务的 commit ts 是单调的。
2. 调用 `oracle.newCommitTs` , oracle 会维护: 事务冲突 + 读写水位, 然后逻辑上是线程安全的.
3. 给事务的 key 编码上版本（以 bigendian u64 的形式), 给事务加上 txn 的标记, 然后加上 FinTxn 的事务
4. 调用 `db.sendToWriteCh`, 这里要用确定的时序发送给写入 Memtable 和 LSM 的线程.
5. 调用 `doneCommit`, 这里会恢复水位, 通知卡着的读事务.

`db.sendToWriteCh` 也很有意思, 还用到了 pool, 自己管理了一堆逻辑. 这里给写入做了一些限制(可能返回错误, 不过我觉得概率不会很高), 然后从 pool 里面拿出了一个 `request*` 对象:

```go
type request struct {
	// Input values
	Entries []*Entry
	// Output values and wait group stuff below
	Ptrs []valuePointer
	Wg   sync.WaitGroup
	Err  error
	ref  int32
}
```

`sendToWriteCh` 签名如下:

```go
func (db *DB) sendToWriteCh(entries []*Entry) (*request, error)
```

可以看到, 这里送进来的是归属于同一个事务、带 `bitTxn` 的事务组, 和一个 `bitTxnFin`. 这里初始化的时候, 逻辑如下:

```go
req := requestPool.Get().(*request)
req.reset()
req.Entries = entries
req.Wg.Add(1)
req.IncrRef()     // for db write
db.writeCh <- req // Handled in doWrites.
```

这里 `requestPool` 要保证调用的时候, 里面内容全是空的(包括 wg, ref, 其他内容倒是无所谓). 然后这里丢给 `req.Entries`, `Ptrs` 目前没有初始化. 然后发给 `writeCh` .

然后我们且看 `DB.doWrites`, 这个地方起了一个 goroutine 来处理任务. 这里与外部的 `writeCh` 和 `Closer` 交互, 内部创建了一个 `pendingCh`, pending 的时候, `request` 会被打包到一起:

```go
reqs = append(reqs, r)
reqLen.Set(int64(len(reqs)))
```

这里 `reqLen` 太长也会阻塞. 然后, 要写入的时候, 这里会占据 `pendingCh`, 直到写完:

```go
writeRequests := func(reqs []*request) {
	if err := db.writeRequests(reqs); err != nil {
		db.opt.Errorf("writeRequests: %v", err)
	}
	<-pendingCh
}
```

具体写入的时候:


```go
writeCase:
	go writeRequests(reqs)
	// 为写创建空间.
	reqs = make([]*request, 0, 10)
	reqLen.Set(0)
```

这里把写入委托给了下面的函数, 注意参数已经变成了 `request` 的数组了:

```go
// writeRequests is called serially by only one goroutine.
func (db *DB) writeRequests(reqs []*request) error
```

这个函数内容是写 vLog (`db.vlog.write(reqs)`) -> 对每个请求写 LSM (`for every b in req: db.writeToLSM(b)`). 比较有意思的还有这个单独 `writeToLSM`, 感觉主要是因为每个内容都很小, 独立写就独立写了(LevelDB 搞成了一整个二进制再写). 我们来看看细节吧:

#### Value Log

这里首先写的是 value log，比较重要的函数是：

```go
func (vlog *valueLog) write(reqs []*request) error
```

这里面会短暂上 `valueLog.filesLock`, 外部应该会保证 valueLog 只有一个 writer. 这里函数处理的参数是 `reqs`, 大概的逻辑是：

1. 检查是否对得上 vlog 的分离阈值（目前版本是 1M 大小），对得上就拆出来。然后更新 request 的 `Ptrs` 字段。这里无论有没有 vlog 都会加入 `Ptrs`
2. 写到 value log 的内容是没有 txn 相关的 meta 的
3. 写下去的内容格式如下，我真心感觉没必要搞 varint, 不过这种东西写了也没法改了。

```go
// +--------+-----+-------+-------+
// | header | key | value | crc32 |
// +--------+-----+-------+-------+

// header is used in value log as a header before Entry.
// 可以把 Header 视作一个要写的数据, 两个 byte 会以 varint 的形式写入(有必要吗, 省这点？),
// 长度由 `maxHeaderSize` 指定, kv 分离大小是 1k->1M, 所以感觉多写点东西也没啥.
type header struct {
	klen      uint32
	vlen      uint32
	expiresAt uint64
	meta      byte
	userMeta  byte
}
```

vLog 大概就是这部分逻辑了，此外还有一些检查 value 大小什么的，感觉比较 trivial.

#### 写入 memtable

这里是前台写入最后一步，这里本来很简单，不过有些细节可以拉出来聊聊，这里大概流程如下：

```
// 串行写 memtable:
// 1. 确保空间足够. 这里比较有意思的是, 没有 WriteStall 降速.
// 2. writeToLSM 来写具体内容.
// 3. 写入的时候(包括写 vLog + mem+wal) 都是独立串行的.
```

这里没有任何的 write stall, 只会在写满的时候尝试 sleep 10ms，我个人感觉是因为写入的时候，很多瓶颈理应在 value log, 但是上面没有降速感觉还是不太合理。

第二个比较有意思的地方是，这里来的请求是 requests, 但是 `writeToLSM` 是单个 request 为粒度写的。

```go
// writeToLSM 的单位是 request. 这点本身没什么问题, 但是这里会 SyncWAL, 感觉有点怪
// 感觉有点好处就是 db.lock 的粒度比较小.
// TODO(mwish): 为什么这里要 sync WAL 呢?
// A: 我个人感觉上是为了不影响前台 mt 的读.
func (db *DB) writeToLSM(b *request) error {
	// 这个 lock 保护了 db.mt, 让 db.mt 是一写多读的.
	db.lock.RLock()
	defer db.lock.RUnlock()
	...
}
```

最后，这里引入了一个 `ValueStruct`, 我们大概可以推测出 Badger 提供的模型是什么样子的了：

```go
// ValueStruct represents the value info that can be associated with a key, but also the internal
// Meta field.
//
// ValueStruct 在写入 Memtable 和 WAL 的时候被启用. 感觉是 Entry 去掉了 key 包了一层.
// 这么一看 Badger 存储的模型应该是: <Key, ValueStruct>.
type ValueStruct struct {
	Meta      byte
	UserMeta  byte
	ExpiresAt uint64
	// Value 存储的是下面之一:
	// 1. 指向 vLog 的 ptr
	// 2. Value
	// 上面两者需要靠 Meta 的 bitValuePointer 区分。
	Value     []byte

	Version uint64 // This field is not serialized. Only for internal usage.
}
```

上面的部分讲完了前台的写入，但是没有对后台的 Compaction

## Flush L0

## GC

## Compaction

## 读取



