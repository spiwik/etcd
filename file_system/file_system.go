package fileSystem

import (
	"path"
	"strings"
	"time"

	etcdErr "github.com/coreos/etcd/error"
)

type FileSystem struct {
	Root         *Node
	EventHistory *EventHistory
	WatcherHub   *watcherHub
	Index        uint64
	Term         uint64
}

func New() *FileSystem {
	return &FileSystem{
		Root:       newDir("/", 0, 0, nil, ""),
		WatcherHub: newWatchHub(1000),
	}

}

func (fs *FileSystem) Get(keyPath string, recusive bool, index uint64, term uint64) (*Event, error) {
	// TODO: add recursive get
	n, err := fs.InternalGet(keyPath, index, term)

	if err != nil {
		return nil, err
	}

	e := newEvent(Get, keyPath, index, term)

	if n.IsDir() { // node is dir
		e.KVPairs = make([]KeyValuePair, len(n.Children))

		i := 0

		for _, child := range n.Children {

			if child.IsHidden() { // get will not list hidden node
				continue
			}

			e.KVPairs[i] = child.Pair()

			i++
		}

		// eliminate hidden nodes
		e.KVPairs = e.KVPairs[:i]

	} else { // node is file
		e.Value = n.Value
	}

	return e, nil
}

func (fs *FileSystem) Set(keyPath string, value string, expireTime time.Time, index uint64, term uint64) (*Event, error) {
	keyPath = path.Clean("/" + keyPath)

	// update file system known index and term
	fs.Index, fs.Term = index, term

	dir, name := path.Split(keyPath)

	// walk through the keyPath and get the last directory node
	d, err := fs.walk(dir, fs.checkDir)

	if err != nil {
		return nil, err
	}

	e := newEvent(Set, keyPath, fs.Index, fs.Term)
	e.Value = value

	f, err := d.GetFile(name)

	if err == nil {

		if f != nil { // update previous file if exist
			e.PrevValue = f.Value
			f.Write(e.Value)

			// if the previous ExpireTime is not Permanent and expireTime is given
			// we stop the previous expire routine
			if f.ExpireTime != Permanent && expireTime != Permanent {
				f.stopExpire <- true
			}
		} else { // create new file

			f = newFile(keyPath, value, fs.Index, fs.Term, d, "", expireTime)

			err = d.Add(f)

		}

	}

	if err != nil {
		return nil, err
	}

	// Node with TTL
	if expireTime != Permanent {
		go f.Expire()
		e.Expiration = &f.ExpireTime
		e.TTL = int64(expireTime.Sub(time.Now()) / time.Second)
	}

	return e, nil
}

func (fs *FileSystem) TestAndSet(keyPath string, prevValue string, prevIndex uint64, index uint64, term uint64) {

}

func (fs *FileSystem) TestIndexAndSet() {

}

func (fs *FileSystem) Delete(keyPath string, recurisive bool, index uint64, term uint64) (*Event, error) {
	n, err := fs.InternalGet(keyPath, index, term)

	if err != nil {
		return nil, err
	}

	err = n.Remove(recurisive)

	if err != nil {
		return nil, err
	}

	e := newEvent(Delete, keyPath, index, term)

	if n.IsDir() {
		e.Dir = true
	} else {
		e.PrevValue = n.Value
	}

	return e, nil
}

// walk function walks all the keyPath and apply the walkFunc on each directory
func (fs *FileSystem) walk(keyPath string, walkFunc func(prev *Node, component string) (*Node, error)) (*Node, error) {
	components := strings.Split(keyPath, "/")

	curr := fs.Root

	var err error
	for i := 1; i < len(components); i++ {
		if len(components[i]) == 0 { // ignore empty string
			return curr, nil
		}

		curr, err = walkFunc(curr, components[i])
		if err != nil {
			return nil, err
		}

	}

	return curr, nil
}

// InternalGet function get the node of the given keyPath.
func (fs *FileSystem) InternalGet(keyPath string, index uint64, term uint64) (*Node, error) {
	keyPath = path.Clean("/" + keyPath)

	// update file system known index and term
	fs.Index, fs.Term = index, term

	walkFunc := func(parent *Node, dirName string) (*Node, error) {
		child, ok := parent.Children[dirName]
		if ok {
			return child, nil
		}

		return nil, etcdErr.NewError(100, "get")
	}

	f, err := fs.walk(keyPath, walkFunc)

	if err != nil {
		return nil, err
	}

	return f, nil
}

// checkDir function will check whether the component is a directory under parent node.
// If it is a directory, this function will return the pointer to that node.
// If it does not exist, this function will create a new directory and return the pointer to that node.
// If it is a file, this function will return error.
func (fs *FileSystem) checkDir(parent *Node, dirName string) (*Node, error) {

	subDir, ok := parent.Children[dirName]

	if ok {
		return subDir, nil
	}

	n := newDir(path.Join(parent.Path, dirName), fs.Index, fs.Term, parent, parent.ACL)

	parent.Children[dirName] = n

	return n, nil
}
