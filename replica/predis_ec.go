package replica

import (
	"bytes"
	"fmt"
	rs "github.com/templexxx/reedsolomon"
	"math"
	"math/rand"
	"time"
)

type EcEngine struct {
	rsEngine   *rs.RS
	dataNum    int
	parityNum  int
	replicaNum int
}

func newEcEngine(dataNum int, parityNum int) *EcEngine {
	var eceg EcEngine
	rsTest, err := rs.New(dataNum, parityNum)
	if err == nil {
		eceg.rsEngine = rsTest
	} else {
		fmt.Println(err)
	}
	eceg.dataNum = dataNum
	eceg.parityNum = parityNum
	eceg.replicaNum = dataNum + parityNum

	return &eceg
}
func (eceg *EcEngine) randCreator(l int) string {
	str := "0123456789abcdefghigklmnopqrstuvwxyz"
	strList := []byte(str)

	var result []byte
	i := 0

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i < l {
		new := strList[r.Intn(len(strList))]
		result = append(result, new)
		i = i + 1
	}
	return string(result)
}
func (eceg *EcEngine) CreateEcMatrix(batchInByte []byte) (ecMatrix [][]byte, realLen []int32) {
	batchInBytelength := len(batchInByte)
	rowLen := int(math.Ceil(float64(batchInBytelength) / float64(eceg.dataNum)))
	realLen = make([]int32, eceg.replicaNum)
	ecMatrix = make([][]byte, eceg.replicaNum)
	for i := 0; i < eceg.replicaNum; i++ {
		ecMatrix[i] = make([]byte, rowLen)
	}
	from := int(0)
	to := int(0)
	for i := 0; i < eceg.dataNum; i++ {
		from = i * rowLen
		to = from + rowLen
		if to <= len(batchInByte) {
			ecMatrix[i] = batchInByte[from:to]
			realLen[i] = int32(rowLen)
		} else {
			var buffer bytes.Buffer
			if from < batchInBytelength {
				buffer.Write(batchInByte[from:batchInBytelength])
				buffer.Write([]byte(eceg.randCreator(rowLen - (batchInBytelength - from))))
				ecMatrix[i] = buffer.Bytes()
				realLen[i] = int32(batchInBytelength - from)
			} else {
				ecMatrix[i] = []byte(eceg.randCreator(rowLen))
				realLen[i] = 0
			}

		}

	}
	for i := eceg.dataNum; i < eceg.parityNum; i++ {
		ecMatrix[i] = []byte(eceg.randCreator(rowLen))
		realLen[i] = 0
	}
	err := eceg.rsEngine.Encode(ecMatrix)
	if err != nil {
		fmt.Println(err)
	}
	return ecMatrix, realLen
}
func (eceg *EcEngine) EcDecode(ecMatrix [][]byte, dphas, needReconst []int, realLen []int32) (batchinbyte []byte) {
	err := eceg.rsEngine.Reconst(ecMatrix, dphas, needReconst)
	if err != nil {
		fmt.Println(err)
	} else {
		var buffer bytes.Buffer
		for i := 0; i < eceg.dataNum; i++ {
			buffer.Write(ecMatrix[i][0:realLen[i]])
		}
		batchinbyte = buffer.Bytes()
		return batchinbyte
	}
	return batchinbyte
}
