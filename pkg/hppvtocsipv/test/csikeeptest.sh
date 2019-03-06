#!/bin/bash 
kubeconfig=/home/adam/.kube/zjconfig
pv1="./keeptruepv.yaml"
pv2="./keeptruepv2.yaml"
pvc1="./keeptruepvc.yaml"
pvc2="./keeptruepvc2.yaml"
app="./testapp.yaml"
namespace="patricktest"
pvName1="keeptruepv"
pvName2="keeptruepv2"
deleteCheckTime=3
appName="csi-hostpathpv-test"
replica=6
k="kubectl --kubeconfig=$kubeconfig"

fClean()
{
    $k delete -f $app 2>/dev/null 1>/dev/null
    fWaitPodDeleted 2>/dev/null 1>/dev/null
    $k delete -f $pvc2 2>/dev/null 1>/dev/null
    $k delete -f $pvc1 2>/dev/null 1>/dev/null
    $k delete -f $pv1 2>/dev/null 1>/dev/null
    $k delete -f $pv2 2>/dev/null 1>/dev/null
 
    sleep 30

    while true
    do
      pv1Exist=`$k get pv 2>&1 | grep $pvName1`
      pv2Exist=`$k get pv 2>&1 | grep $pvName2`
     
      if [ "$pv1Exist" != "" ] || [ "$pv2Exist" != "" ]; then
          sleep 1
      else
          echo "OK"
          return
      fi
    done
}

fWaitPVBound()
{
   while true
   do
      statue1=`$k get pv $pvName1 | grep $pvName1 | awk '{print $5}'`
      statue2=`$k get pv $pvName2 | grep $pvName2 | awk '{print $5}'`
      if [ "$statue1" == "Bound" ] && [ "$statue2" == "Bound" ]; then
         return
      fi
      sleep 1
   done
}

fCreate()
{
   $k create -f $pv1 2>/dev/null 1>/dev/null
   $k create -f $pv2 2>/dev/null 1>/dev/null
   $k create -f $pvc1 2>/dev/null 1>/dev/null
   $k create -f $pvc2 2>/dev/null 1>/dev/null
   fWaitPVBound
   $k create -f $app 2>/dev/null 1>/dev/null

   echo "OK"
}

fWaitPodDeleted()
{
   while true
   do
      existpods=`$k -n $namespace get pod | grep $appName | wc -l`
      if (($existpods == 0)); then
         return
      else
         sleep 1
      fi
   done
}

fWaitPodRunning()
{
  while true
   do
       flag="true"
       for state in `$k -n $namespace get pod | grep $appName | awk '{print $3}'`
       do
          if [ "$state" != "Running" ]; then
              flag="false"
              break
          fi
       done
       if [ "$flag" == "true" ]; then
          echo "OK"
          return
       else
           sleep 1
       fi
   done
}

fdeletePods() 
{
  for podname in `$k -n $namespace get pod | grep $appName | awk '{print $1}'`
  do
     $k -n $namespace delete pod $podname 2>/dev/null 1>/dev/null
  done
}

fWaitPVUpdate()
{
   timeOut=$1
   deletePods=$2
   count=0
   while true
   do
      pvMountNum1=`$k hostpathpv describe pv $pvName1 | grep "/xfs" | awk '{print $2}' | grep -E "$namespace/$appName" | wc -l`
      pvMountNum2=`$k hostpathpv describe pv $pvName2 | grep "/xfs" | awk '{print $2}' | grep -E "$namespace/$appName" | wc -l`

      if (($pvMountNum1 == $replica)) && (($pvMountNum2 == $replica)); then
         echo "OK"
         return
      fi

      count=`expr $count + 1`
      if (($count >= $timeOut)); then
         if [ "$deletePods" == "true" ]; then
           deletePods="false"
           count=0
           for podname in `$k -n $namespace get pod | grep $appName | awk '{print $1}'`
           do
             $k -n $namespace delete pod $podname 2>/dev/null 1>/dev/null
           done
         else
           echo "TimeOut"
           return
         fi
      else
         sleep 1
      fi
   done 
}

fWriteTestData()
{
    for info in `$k hostpathpv describe pv $pvName1 | grep "/xfs" | awk '{print $1":"$2":"$3}'`
    do
       path=`echo $info | awk -F: '{print $1}'`
       podPath=`echo $info | awk -F: '{print $4}'`
       podname=`echo $info | awk -F: '{print $2}' | awk -F/ '{print $2}'`
       $k -n $namespace exec -it $podname -- sh -c "echo -n $path > $podPath/path.txt"
    done

    for info in `$k hostpathpv describe pv $pvName2 | grep "/xfs" | awk '{print $1":"$2":"$3}'`
    do
       path=`echo $info | awk -F: '{print $1}'`
       podPath=`echo $info | awk -F: '{print $4}'`
       podname=`echo $info | awk -F: '{print $2}' | awk -F/ '{print $2}'`
       $k -n $namespace exec -it $podname -- sh -c "echo -n $path > $podPath/path.txt"
    done
    echo "OK"
}

fWriteDataCheck()
{
    for((i=0;i<4;i++))
    do
       if [ "`writeDataCheck`" == "OK" ]; then
           echo "OK"
           return
       fi
       sleep 1
    done
    echo "wait time out"
}

writeDataCheck()
{
    for info in `$k hostpathpv describe pv $pvName1 | grep "/xfs" | awk '{print $1":"$2":"$3}'`
    do
       path=`echo $info | awk -F: '{print $1}'`
       podPath=`echo $info | awk -F: '{print $4}'`
       podname=`echo $info | awk -F: '{print $2}' | awk -F/ '{print $2}'`
       read=`$k -n $namespace exec -it $podname -- cat $podPath/path.txt`
       if [ "$path" != "$read" ] && [ "$path/" != "$read" ]; then
           echo "pod $podname [$path] not equal [$read]"
           return
       fi
    done
    echo "OK"
    return
}


fOnOKExist() 
{ 
   start_seconds=$2
   end_seconds=$(date --date="`date +'%Y-%m-%d %H:%M:%S'`" +%s)
   
   if [ "$1" == "OK" ]; then
     echo "OK ($((end_seconds-start_seconds)) s)"
     return
   else
     echo $*
     exit -1
   fi
}

echo $(pwd)
if [ "$1" == "clean" ]; then
   fOnOKExist $(fClean)
elif [ "$1" == "create" ]; then
   fOnOKExist $(fCreate)
else
   testCount=1
   while true
   do
      starttime=`date +'%Y-%m-%d %H:%M:%S'`
# step 1
      echo "Start the $testCount's test" 
      echo -n "  Step 1: clean pods and pvs "
      start=$(date --date="`date +'%Y-%m-%d %H:%M:%S'`" +%s)
      fOnOKExist $(fClean) $start
# step 2
      echo -n "  Step 2: create pvs and pods "
      start=$(date --date="`date +'%Y-%m-%d %H:%M:%S'`" +%s)
      fOnOKExist $(fCreate) $start
# step 3
      echo -n "  Step 3: wait pod running "
      start=$(date --date="`date +'%Y-%m-%d %H:%M:%S'`" +%s)
      fOnOKExist $(fWaitPodRunning) $start
# step 4
      echo -n "  Step 4: wait pv update "
      start=$(date --date="`date +'%Y-%m-%d %H:%M:%S'`" +%s)
      fOnOKExist $(fWaitPVUpdate 60) $start
# step 4
      echo -n "  Step 5: start write test data "
      start=$(date --date="`date +'%Y-%m-%d %H:%M:%S'`" +%s)
      fOnOKExist $(fWriteTestData) $start
# step 5
      echo -n "  Step 6: check write test data "
      start=$(date --date="`date +'%Y-%m-%d %H:%M:%S'`" +%s)
      fOnOKExist $(fWriteDataCheck) $start
   for ((i=0; i<$deleteCheckTime; i++))
   do
# step 6
      echo -n "  Step 7: start delete and create pods "
      start=$(date --date="`date +'%Y-%m-%d %H:%M:%S'`" +%s)
      fdeletePods
      fOnOKExist "OK" $start
# step 7
      echo -n "  Step 8: wait pod running "
      start=$(date --date="`date +'%Y-%m-%d %H:%M:%S'`" +%s)
      fOnOKExist $(fWaitPodRunning) $start
# step 8
      echo -n "  Step 9: wait pv update "
      start=$(date --date="`date +'%Y-%m-%d %H:%M:%S'`" +%s)
      fOnOKExist $(fWaitPVUpdate 60) $start
# step 9
      echo -n "  Step 10: check write test data "
      start=$(date --date="`date +'%Y-%m-%d %H:%M:%S'`" +%s)
      fOnOKExist $(fWriteDataCheck) $start
    done
      testCount=`expr $testCount + 1`
      endtime=`date +'%Y-%m-%d %H:%M:%S'`
      start_seconds=$(date --date="$starttime" +%s);
      end_seconds=$(date --date="$endtime" +%s);
      echo "本次运行时间： "$((end_seconds-start_seconds))"s"
  done
fi
