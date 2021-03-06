package replica

import (
	"fmt"
	"github.com/openebs/jiva/types"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/Sirupsen/logrus"
	"github.com/rancher/sparse-tools/sparse"
)

const (
	revisionCounterFile             = "revision.counter"
	revisionFileMode    os.FileMode = 0600
	revisionBlockSize               = 4096
)

func (r *Replica) readRevisionCounter() (int64, error) {
	if r.revisionFile == nil {
		return 0, fmt.Errorf("BUG: revision file wasn't initialized")
	}

	buf := make([]byte, revisionBlockSize)
	_, err := r.revisionFile.ReadAt(buf, 0)
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("fail to read from revision counter file: %v", err)
	}
	counter, err := strconv.ParseInt(strings.Trim(string(buf), "\x00"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("fail to parse revision counter file: %v", err)
	}
	return counter, nil
}

func (r *Replica) writeRevisionCounter(counter int64) error {
	if r.revisionFile == nil {
		return fmt.Errorf("BUG: revision file wasn't initialized")
	}

	buf := make([]byte, revisionBlockSize)
	copy(buf, []byte(strconv.FormatInt(counter, 10)))
	_, err := r.revisionFile.WriteAt(buf, 0)
	if err != nil {
		return fmt.Errorf("fail to write to revision counter file: %v", err)
	}
	return nil
}

func (r *Replica) openRevisionFile(isCreate bool) error {
	var err error
	r.revisionFile, err = sparse.NewDirectFileIoProcessor(r.diskPath(revisionCounterFile), os.O_RDWR, revisionFileMode, isCreate)
	return err
}

func (r *Replica) initRevisionCounter() error {
	r.revisionLock.Lock()
	defer r.revisionLock.Unlock()

	if _, err := os.Stat(r.diskPath(revisionCounterFile)); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		// file doesn't exist yet
		if err := r.openRevisionFile(true); err != nil {
			logrus.Errorf("failed to open revision counter file")
			return err
		}
		if err := r.writeRevisionCounter(1); err != nil {
			logrus.Errorf("failed to update revision counter")
			return err
		}
	} else if err := r.openRevisionFile(false); err != nil {
		logrus.Errorf("open existing revision counter file failed")
		return err
	}

	counter, err := r.readRevisionCounter()
	if err != nil {
		logrus.Errorf("failed to read revision counter")
		return err
	}
	// Don't use r.revisionCache directly
	// r.revisionCache is an internal cache, to avoid read from disk
	// everytime when counter needs to be updated.
	// And it's protected by revisionLock
	r.revisionCache = counter
	return nil
}

func (r *Replica) GetRevisionCounter() int64 {
	r.revisionLock.Lock()
	defer r.revisionLock.Unlock()

	counter, err := r.readRevisionCounter()
	if err != nil {
		logrus.Error("Fail to get revision counter: ", err)
		// -1 will result in the replica to be discarded
		return -1
	}
	r.revisionCache = counter
	return counter
}

func (r *Replica) SetRevisionCounter(counter int64) error {
	r.Lock()
	if r.mode != types.RW {
		r.Unlock()
		return fmt.Errorf("setting revisioncounter during %v mode is invalid", r.mode)
	}
	r.Unlock()
	r.revisionLock.Lock()
	defer r.revisionLock.Unlock()

	if err := r.writeRevisionCounter(counter); err != nil {
		return err
	}

	r.revisionCache = counter
	return nil
}

func (r *Replica) increaseRevisionCounter() error {
	r.revisionLock.Lock()
	defer r.revisionLock.Unlock()

	if err := r.writeRevisionCounter(r.revisionCache + 1); err != nil {
		return err
	}

	r.revisionCache++
	return nil
}
