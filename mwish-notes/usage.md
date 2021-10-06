badger 采用了 Option chains 的方式做打开的配置，如：

```
func Open(opt Options) (*DB, error)
```

这里的 Options 是一个如下的结构体：

```go
// Note: If you add a new option X make sure you also add a WithX method on Options.

// Options are params for creating DB object.
//
// This package provides DefaultOptions which contains options that should
// work for most applications. Consider using that as a starting point before
// customizing it for your own needs.
//
// Each option X is documented on the WithX method.
type Options struct {...}
```

我们稍后会介绍这些配置。

打开的操作如下：

```go
  // Open the Badger database located in the /tmp/badger directory.
  // It will be created if it doesn't exist.
  db, err := badger.Open(badger.DefaultOptions("/tmp/badger"))
  if err != nil {
	  log.Fatal(err)
  }
  defer db.Close()
```

这里允许使用 in-memory 模式，但是我们对这个模式没那么感兴趣。这里还有个 `IndexCache`, 作为缓存。

同时，系统还可以开启加密模式 `Encryption Mode`, 在启动的时候，codec 代价会变高，所以推荐打开 IndexCache，作为文件相关的基础缓存

这里读写和 bolt 表面上差不多：

```go
// 只读
err := db.View(func(txn *badger.Txn) error {
  // Your code here…
  return nil
})

// 读写
err := db.Update(func(txn *badger.Txn) error {
  // Your code here…
  return nil
})
```

但是实际上，读的时候，Bolt 这些应该拿到的是完整的 `key/value` 对，而 badger 拿到的是一个 `Item`, 这个 Item 会存在，是因为 prefetch 等至关重要的优化。以及 badger 引擎的限制。

这里的内部操作的都是 kv-pair 的形式。这里应该有 `txn.Set` `txn.Get` 等处理 kv 的内容。`DB.GetSequence` 可以获得一个 seq 来处理内容。

很奇怪的是，这里还支持了 `MergeOperator`, 这个东西相当于用户层的支持 rwn，即用户定义对某个值的 读-改-写badger 的流程。这个应该是引擎上层的工作。

```go
// Merge function to append one byte slice to another
func add(originalValue, newValue []byte) []byte {
  return append(originalValue, newValue...)
}

key := []byte("merge")

m := db.GetMergeOperator(key, add, 200*time.Millisecond)
defer m.Stop()

m.Add([]byte("A"))
m.Add([]byte("B"))
m.Add([]byte("C"))

res, _ := m.Get() // res should have value ABC encoded
```

可以看出，这是一个 rwn 的操作，给用户做了一层读写事务的封装。

此外，这里还 user-space 的支持了 `TTL`:

```go
err := db.Update(func(txn *badger.Txn) error {
  e := badger.NewEntry([]byte("answer"), []byte("42")).WithTTL(time.Hour)
  err := txn.SetEntry(e)
  return err
})
```

这里也支持 `Scan` 和 `Iterator` 等操作，当然也支持 key-only iterator.

此外，有两个比较重要的功能：streaming / gc vlog.

streaming 会并发扫描所有的 key，然后给对应的 target 甚至写回。gc vlog 会对 value log 做 garbage collection.

---

上面有一点模糊的是，badger 支持了事务，具体的 blog 在：https://dgraph.io/blog/post/badger-txn/

---

Options:

```go
// Note: If you add a new option X make sure you also add a WithX method on Options.

// Options are params for creating DB object.
//
// This package provides DefaultOptions which contains options that should
// work for most applications. Consider using that as a starting point before
// customizing it for your own needs.
//
// Each option X is documented on the WithX method.
type Options struct {
	// Required options.

	Dir      string
	ValueDir string

	// Usually modified options.

	SyncWrites        bool
	NumVersionsToKeep int
	ReadOnly          bool
	Logger            Logger
	Compression       options.CompressionType
	InMemory          bool
	MetricsEnabled    bool
	// Sets the Stream.numGo field
	NumGoroutines int

	// Fine tuning options.

	MemTableSize        int64
	BaseTableSize       int64
	BaseLevelSize       int64
	LevelSizeMultiplier int
	TableSizeMultiplier int
	MaxLevels           int

	VLogPercentile float64
	ValueThreshold int64
	NumMemtables   int
	// Changing BlockSize across DB runs will not break badger. The block size is
	// read from the block index stored at the end of the table.
	BlockSize          int
	BloomFalsePositive float64
	BlockCacheSize     int64
	IndexCacheSize     int64

	NumLevelZeroTables      int
	NumLevelZeroTablesStall int

	ValueLogFileSize   int64
	ValueLogMaxEntries uint32

	NumCompactors        int
	CompactL0OnClose     bool
	LmaxCompaction       bool
	ZSTDCompressionLevel int

	// When set, checksum will be validated for each entry read from the value log file.
	VerifyValueChecksum bool

	// Encryption related options.
	EncryptionKey                 []byte        // encryption key
	EncryptionKeyRotationDuration time.Duration // key rotation duration

	// BypassLockGuard will bypass the lock guard on badger. Bypassing lock
	// guard can cause data corruption if multiple badger instances are using
	// the same directory. Use this options with caution.
	BypassLockGuard bool

	// ChecksumVerificationMode decides when db should verify checksums for SSTable blocks.
	ChecksumVerificationMode options.ChecksumVerificationMode

	// AllowStopTheWorld determines whether the DropPrefix will be blocking/non-blocking.
	AllowStopTheWorld bool

	// DetectConflicts determines whether the transactions would be checked for
	// conflicts. The transactions can be processed at a higher rate when
	// conflict detection is disabled.
	DetectConflicts bool

	// NamespaceOffset specifies the offset from where the next 8 bytes contains the namespace.
	NamespaceOffset int

	// Transaction start and commit timestamps are managed by end-user.
	// This is only useful for databases built on top of Badger (like Dgraph).
	// Not recommended for most users.
	managedTxns bool

	// 4. Flags for testing purposes
	// ------------------------------
	maxBatchCount int64 // max entries in batch
	maxBatchSize  int64 // max batch size in bytes

	maxValueThreshold float64
}
```



