package cmd

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	xfs "k8s-plugins/csi-plugin/hostpathpv/pkg/hostpath/xfsquotamanager/common"
	"k8s-plugins/extender-scheduler/pkg/algorithm"
	"strconv"
	"strings"
	"time"

	"github.com/chai2010/gettext-go/gettext"
	"github.com/golang/glog"
	"github.com/spf13/cobra"
	"k8s.io/api/core/v1"
)

//func LongDesc(s string) string {
//	if len(s) == 0 {
//		return s
//	}
//	return normalizer{s}.heredoc().markdown().trim().string
//}

func T(defaultValue string, args ...int) string {
	if len(args) == 0 {
		return gettext.PGettext("", defaultValue)
	}
	return fmt.Sprintf(gettext.PNGettext("", defaultValue, defaultValue+".plural", args[0]),
		args[0])
}

func UsageErrorf(cmd *cobra.Command, format string, args ...interface{}) error {
	msg := fmt.Sprintf(format, args...)
	return fmt.Errorf("%s\nSee '%s -h' for help and examples.", msg, cmd.CommandPath())
}

func GetFlagBool(cmd *cobra.Command, flag string) bool {
	b, err := cmd.Flags().GetBool(flag)
	if err != nil {
		glog.Fatalf("error accessing flag %s for command %s: %v", flag, cmd.Name(), err)
	}
	return b
}

func GetFlagDuration(cmd *cobra.Command, flag string) time.Duration {
	d, err := cmd.Flags().GetDuration(flag)
	if err != nil {
		glog.Fatalf("error accessing flag %s for command %s: %v", flag, cmd.Name(), err)
	}
	return d
}

// Assumes the flag has a default value.
func GetFlagInt(cmd *cobra.Command, flag string) int {
	i, err := cmd.Flags().GetInt(flag)
	if err != nil {
		glog.Fatalf("error accessing flag %s for command %s: %v", flag, cmd.Name(), err)
	}
	return i
}

func GetFlagString(cmd *cobra.Command, flag string) string {
	s, err := cmd.Flags().GetString(flag)
	if err != nil {
		glog.Fatalf("error accessing flag %s for command %s: %v", flag, cmd.Name(), err)
	}
	return s
}

func maxint(a, b int) int {
	if a > b {
		return a
	} else {
		return b
	}
}

type printFun func()

var printFunChan chan printFun

func init() {
	printFunChan = make(chan printFun, 10)
	go func() {
		for {
			select {
			case fun := <-printFunChan:
				fun()
			}
		}
	}()
}
func getFromReader(r io.Reader, laststrLen *int) <-chan string {
	strChan := make(chan string, 0)
	lineReader := bufio.NewReader(r)
	go func() {
		for {
			line, _, err := lineReader.ReadLine()
			if err != nil {
				//strChan <- ""
				//close(strChan)
			} else {
				if strings.Contains(string(line), "Has moved") {
					tmp := string("[ ") + string(line) + string(" ]")
					strChan <- tmp
					*laststrLen = len(tmp)
				}
			}
		}
	}()
	return strChan
}
func getDelayStyle1(sum int, interval time.Duration, stop <-chan struct{}) <-chan string {
	strChan := make(chan string, 0)
	go func() {
		i := 1
	loopfor:
		for {
			select {
			case <-stop:
				strChan <- ""
				break loopfor
			default:
				strChan <- fmt.Sprintf(" (%d/%d)", i, sum)
				i++
			}
			time.Sleep(interval)
		}
	}()
	return strChan
}
func printTimeDelay(strChan <-chan string, stop <-chan struct{}) {
	lastStr := ""
	go func() {
	loopfor:
		for {
			select {
			case <-stop:
				printFunChan <- func() {
					fmt.Printf("%s", strings.Repeat("\b", len(lastStr)))
				}
				break loopfor
			case str := <-strChan:
				printFunChan <- func() {
					fmt.Printf("%s%s", strings.Repeat("\b", len(lastStr)), str)
					lastStr = str
				}
			}
		}
	}()
}
func stepPrintf(statueChan <-chan string, format string, a ...interface{}) {
	buf := fmt.Sprintf(format, a...)
	//fmt.Printf("%s", buf)
	printFunChan <- func() {
		fmt.Printf("%s", buf)
	}
	go func() {
		l := len(buf)
	forLoop:
		for {
			select {
			case statue := <-statueChan:
				num, err := strconv.Atoi(statue)
				if err == nil { // is num
					l += num
				} else {
					printFunChan <- func() {
						if 120-l > 0 {
							fmt.Printf("%s[ %s ]\n", strings.Repeat("-", 120-l), statue)
						} else {
							fmt.Printf("[ %s ]\n", statue)
						}
					}
					break forLoop
				}
			}
		}
	}()
}

func getHashStr(strs []string, l int) string {
	hash := md5.New()
	for _, str := range strs {
		hash.Write([]byte(str))
	}
	tmp := hex.EncodeToString(hash.Sum(nil))
	if l >= len(tmp) {
		return tmp
	}
	return tmp[:l]
}

func getPercentStr(used, capacity int64) string {
	if capacity <= 0 {
		return "(00.00%)"
	}
	return fmt.Sprintf("(%.2f%%)", (float64(used)/float64(capacity))*100.0)
}

func getPodInfo(str string) (ns, name, uid string) {
	if str != "" {
		strs := strings.Split(str, ":")
		if len(strs) == 3 {
			return strs[0], strs[1], strs[2]
		}
	}
	return
}

func getHostpathPVType(pv *v1.PersistentVolume) int {
	if pv.Annotations == nil {
		return KeepFalse
	} else if pv.Annotations[xfs.PVHostPathMountPolicyAnn] != xfs.PVHostPathNone &&
		pv.Annotations[xfs.PVHostPathQuotaForOnePod] == "true" {
		return KeepTrue
	} else if pv.Annotations[xfs.PVHostPathMountPolicyAnn] != xfs.PVHostPathNone &&
		pv.Annotations[xfs.PVHostPathQuotaForOnePod] != "true" {
		return KeepFalse
	} else if pv.Annotations[xfs.PVHostPathMountPolicyAnn] == xfs.PVHostPathNone &&
		pv.Annotations[xfs.PVHostPathQuotaForOnePod] != "true" {
		return NoneFalse
	} else {
		return NoneTrue
	}
}

func getCSIHostpathPVType(pv *v1.PersistentVolume) int {
	if pv.Annotations == nil {
		return CSIKeepFalse
	} else if pv.Annotations[xfs.PVHostPathMountPolicyAnn] != xfs.PVHostPathNone &&
		pv.Annotations[xfs.PVHostPathQuotaForOnePod] == "true" {
		return CSIKeepTrue
	} else if pv.Annotations[xfs.PVHostPathMountPolicyAnn] != xfs.PVHostPathNone &&
		pv.Annotations[xfs.PVHostPathQuotaForOnePod] != "true" {
		return CSIKeepFalse
	} else if pv.Annotations[xfs.PVHostPathMountPolicyAnn] == xfs.PVHostPathNone &&
		pv.Annotations[xfs.PVHostPathQuotaForOnePod] != "true" {
		return CSINoneFalse
	} else {
		return CSINoneTrue
	}
}

func getPVType(pv *v1.PersistentVolume) int {
	if algorithm.IsHostPathPV(pv) {
		return getHostpathPVType(pv)
	} else if algorithm.IsCSIHostPathPV(pv) {
		return getCSIHostpathPVType(pv)
	}
	return PVUnknow
}

func convertIntToString(size int64) string {
	var ret string
	if size < 1024 {
		ret = fmt.Sprintf("%d B", size)
	} else if size < 1024*1024 {
		ret = fmt.Sprintf("%.2fKB", float64(size)/1024.0)
	} else if size < 1024*1024*1024 {
		ret = fmt.Sprintf("%.2fMB", float64(size)/(1024.0*1024.0))
	} else if size < 1024*1024*1024*1024 {
		ret = fmt.Sprintf("%.2fGB", float64(size)/(1024.0*1024.0*1024.0))
	} else if size < 1024*1024*1024*1024*1024 {
		ret = fmt.Sprintf("%.2fTB", float64(size)/(1024.0*1024.0*1024.0*1024.0))
	} else {
		ret = fmt.Sprintf("%.2fPB", float64(size)/(1024.0*1024.0*1024.0*1024.0*1024.0))
	}
	if len(ret) < 9 {
		ret = getNumSpace(9-len(ret), " ") + ret
	}
	return ret
}

func getPVCapacity(pv *v1.PersistentVolume) int64 {
	storage, exists := pv.Spec.Capacity[v1.ResourceStorage]
	if exists == false {
		return 0
	}
	capacity := storage.Value()
	if pv.Annotations != nil &&
		pv.Annotations[xfs.PVHostPathCapacityAnn] != "" {
		capacityAnn, errParse := strconv.ParseInt(pv.Annotations[xfs.PVHostPathCapacityAnn], 10, 64)
		if errParse == nil {
			capacity = capacityAnn
		}
	}
	return capacity
}

func getNodeCondition(status *v1.NodeStatus, conditionType v1.NodeConditionType) (int, *v1.NodeCondition) {
	if status == nil {
		return -1, nil
	}
	for i := range status.Conditions {
		if status.Conditions[i].Type == conditionType {
			return i, &status.Conditions[i]
		}
	}
	return -1, nil
}

func getNodeReadyStatus(node *v1.Node) string {
	_, currentReadyCondition := getNodeCondition(&node.Status, v1.NodeReady)
	if currentReadyCondition.Status == v1.ConditionTrue {
		return "Ready"
	} else {
		return "unReady"
	}
}

func getPVTypeStr(pv *v1.PersistentVolume) string {
	t := getPVType(pv)
	switch t {
	case KeepFalse:
		return "KeepFalse"
	case KeepTrue:
		return "KeepTrue"
	case NoneTrue:
		return "NoneTrue"
	case NoneFalse:
		return "NoneFalse"
	case CSIKeepFalse:
		return "CSIKeepFalse"
	case CSIKeepTrue:
		return "CSIKeepTrue"
	case CSINoneTrue:
		return "CSINoneTrue"
	case CSINoneFalse:
		return "CSINoneFalse"
	}
	return "Unknow"
}

//type normalizer struct {
//	string
//}

//func (s normalizer) markdown() normalizer {
//	bytes := []byte(s.string)
//	formatted := blackfriday.Markdown(bytes, &ASCIIRenderer{Indentation: Indentation}, 0)
//	s.string = string(formatted)
//	return s
//}

//func (s normalizer) heredoc() normalizer {
//	s.string = heredoc.Doc(s.string)
//	return s
//}

//func (s normalizer) trim() normalizer {
//	s.string = strings.TrimSpace(s.string)
//	return s
//}

//func (s normalizer) indent() normalizer {
//	indentedLines := []string{}
//	for _, line := range strings.Split(s.string, "\n") {
//		trimmed := strings.TrimSpace(line)
//		indented := Indentation + trimmed
//		indentedLines = append(indentedLines, indented)
//	}
//	s.string = strings.Join(indentedLines, "\n")
//	return s
//}
