package evidence

import (
	"github.com/Autumn-27/artex/internal/testdb"
	"os"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(testdb.Run(m)) }
