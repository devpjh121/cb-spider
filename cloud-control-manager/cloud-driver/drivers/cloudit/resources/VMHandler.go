// Proof of Concepts of CB-Spider.
// The CB-Spider is a sub-Framework of the Cloud-Barista Multi-Cloud Project.
// The CB-Spider Mission is to connect all the clouds with a single interface.
//
//      * Cloud-Barista: https://github.com/cloud-barista
//
// This is a Cloud Driver Example for PoC Test.
//
// by hyokyung.kim@innogrid.co.kr, 2019.08.

package resources

import (
	"errors"
	"fmt"
	cblog "github.com/cloud-barista/cb-log"
	"github.com/cloud-barista/cb-spider/cloud-control-manager/cloud-driver/drivers/cloudit/client"
	"github.com/cloud-barista/cb-spider/cloud-control-manager/cloud-driver/drivers/cloudit/client/ace/server"
	"github.com/cloud-barista/cb-spider/cloud-control-manager/cloud-driver/drivers/cloudit/client/dna/adaptiveip"
	idrv "github.com/cloud-barista/cb-spider/cloud-control-manager/cloud-driver/interfaces"
	irs "github.com/cloud-barista/cb-spider/cloud-control-manager/cloud-driver/interfaces/resources"
	"github.com/sirupsen/logrus"
	"strings"
	"time"
)

var cblogger *logrus.Logger

func init() {
	// cblog is a global variable.
	cblogger = cblog.GetLogger("CB-SPIDER")
}

type ClouditVMHandler struct {
	CredentialInfo idrv.CredentialInfo
	Client         *client.RestClient
}

func (vmHandler *ClouditVMHandler) StartVM(vmReqInfo irs.VMReqInfo) (irs.VMInfo, error) {
	// 가상서버 이름 중복 체크
	vmId, _ := vmHandler.getVmIdByName(vmReqInfo.IId.NameId)
	if vmId != "" {
		errMsg := fmt.Sprintf("VirtualMachine with name %s already exist", vmReqInfo.IId.NameId)
		createErr := errors.New(errMsg)
		return irs.VMInfo{}, createErr
	}

	// 이미지 정보 조회 (Name)
	imageHandler := ClouditImageHandler{
		Client:         vmHandler.Client,
		CredentialInfo: vmHandler.CredentialInfo,
	}
	image, err := imageHandler.GetImage(vmReqInfo.ImageIID)
	if err != nil {
		cblogger.Error(fmt.Sprintf("failed to get image, err : %s", err))
		return irs.VMInfo{}, err
	}

	//  네트워크 정보 조회 (Name)
	VPCHandler := ClouditVPCHandler{
		Client:         vmHandler.Client,
		CredentialInfo: vmHandler.CredentialInfo,
	}
	//vNetwork, err := vNetworkHandler.GetVPC(vmReqInfo.VirtualNetworkId) check modify
	VPC, err := VPCHandler.getSubnetByName(vmReqInfo.SubnetIID.NameId)
	if err != nil {
		cblogger.Error(fmt.Sprintf("failed to get virtual network, err : %s", err))
		return irs.VMInfo{}, err
	}
	/*
		// 보안그룹 정보 조회 (Name)
		securityHandler := ClouditSecurityHandler{
			Client:         vmHandler.Client,
			CredentialInfo: vmHandler.CredentialInfo,
		}
		secGroups := make([]server.SecGroupInfo, len(vmReqInfo.SecurityGroupIIDs))
		for i, s := range vmReqInfo.SecurityGroupIIDs {
			security, err := securityHandler.GetSecurity(s)
			if err != nil {
				cblogger.Error(fmt.Sprintf("failed to get security group, err : %s", err))
				continue
			}
			secGroups[i] = server.SecGroupInfo{
				Id: security.IId.NameId,
			}
		}
	*/
	// Spec 정보 조회 (Name)
	vmSpecId, err := GetVMSpecById(vmHandler.Client.AuthenticatedHeaders(), vmHandler.Client, vmReqInfo.VMSpecName)
	if err != nil {
		cblogger.Error(fmt.Sprintf("failed to get vm spec, err : %s", err))
		return irs.VMInfo{}, err
	}

	vmHandler.Client.TokenID = vmHandler.CredentialInfo.AuthToken
	authHeader := vmHandler.Client.AuthenticatedHeaders()

	/*reqInfo := server.VMReqInfo{
		TemplateId:   vmReqInfo.ImageId,
		SpecId:       vmReqInfo.VMSpecId,
		Name:         vmReqInfo.VMName,
		HostName:     vmReqInfo.VMName,
		RootPassword: vmReqInfo.VMUserPasswd,
		SubnetAddr:   vmReqInfo.VirtualNetworkId,
	}*/
	reqInfo := server.VMReqInfo{
		TemplateId:   image.IId.SystemId,
		SpecId:       *vmSpecId,
		Name:         vmReqInfo.IId.NameId,
		HostName:     vmReqInfo.IId.NameId,
		RootPassword: vmReqInfo.VMUserPasswd,
		SubnetAddr:   VPC.Addr,
		//Secgroups:    secGroups,
	}

	requestOpts := client.RequestOpts{
		MoreHeaders: authHeader,
		JSONBody:    reqInfo,
	}

	// VM 생성
	vm, err := server.Start(vmHandler.Client, &requestOpts)
	if err != nil {
		return irs.VMInfo{}, err
	}

	// VM 생성 완료까지 wait
	vmId = vm.ID
	var serverInfo irs.VMInfo

	for {
		// Check VM Deploy Status
		vmDetailInfo, err := server.Get(vmHandler.Client, vmId, &requestOpts)
		if err != nil {
			return irs.VMInfo{}, err
		}

		if vmDetailInfo.PrivateIp == "" {
			time.Sleep(1 * time.Second)
			continue
		} else {
			if ok, err := vmHandler.AssociatePublicIP(vm.Name, vmDetailInfo.PrivateIp); !ok {
				return irs.VMInfo{}, err
			}
			break
		}
	}
	vmDetailInfo, err := server.Get(vmHandler.Client, vmId, &requestOpts)
	if err != nil {
		return irs.VMInfo{}, err
	}
	serverInfo = mappingServerInfo(*vmDetailInfo)
	//time.Sleep(5 * time.Second)
	return serverInfo, nil
}

func (vmHandler *ClouditVMHandler) SuspendVM(vmIID irs.IID) (irs.VMStatus, error) {
	vmHandler.Client.TokenID = vmHandler.CredentialInfo.AuthToken
	authHeader := vmHandler.Client.AuthenticatedHeaders()

	requestOpts := client.RequestOpts{
		MoreHeaders: authHeader,
	}

	vmID, err := vmHandler.getVmIdByName(vmIID.NameId)
	if err != nil {
		cblogger.Error(err)
		return irs.Failed, err
	}

	if err := server.Suspend(vmHandler.Client, vmID, &requestOpts); err != nil {
		cblogger.Error(err)
		return irs.Failed, err
	}

	// VM 상태 정보 반환
	vmStatus, err := vmHandler.GetVMStatus(vmIID)
	if err != nil {
		cblogger.Error(err)
		return irs.Failed, err
	}
	return vmStatus, nil
}

func (vmHandler *ClouditVMHandler) ResumeVM(vmNameID irs.IID) (irs.VMStatus, error) {
	vmHandler.Client.TokenID = vmHandler.CredentialInfo.AuthToken
	authHeader := vmHandler.Client.AuthenticatedHeaders()

	requestOpts := client.RequestOpts{
		MoreHeaders: authHeader,
	}

	vmID, err := vmHandler.getVmIdByName(vmNameID.NameId)
	if err != nil {
		cblogger.Error(err)
		return irs.Failed, err
	}

	if err := server.Resume(vmHandler.Client, vmID, &requestOpts); err != nil {
		cblogger.Error(err)
		return irs.Failed, err
	}

	// VM 상태 정보 반환
	vmStatus, err := vmHandler.GetVMStatus(vmNameID)
	if err != nil {
		cblogger.Error(err)
		return irs.Failed, err
	}
	return vmStatus, nil
}

func (vmHandler *ClouditVMHandler) RebootVM(vmNameID irs.IID) (irs.VMStatus, error) {
	vmHandler.Client.TokenID = vmHandler.CredentialInfo.AuthToken
	authHeader := vmHandler.Client.AuthenticatedHeaders()

	requestOpts := client.RequestOpts{
		MoreHeaders: authHeader,
	}

	vmID, err := vmHandler.getVmIdByName(vmNameID.NameId)
	if err != nil {
		cblogger.Error(err)
		return irs.Failed, err
	}

	if err := server.Reboot(vmHandler.Client, vmID, &requestOpts); err != nil {
		cblogger.Error(err)
		return irs.Failed, err
	}

	// VM 상태 정보 반환
	vmStatus, err := vmHandler.GetVMStatus(vmNameID)
	if err != nil {
		cblogger.Error(err)
		return irs.Failed, err
	}
	return vmStatus, nil
}

func (vmHandler *ClouditVMHandler) TerminateVM(vmNameID irs.IID) (irs.VMStatus, error) {
	vmHandler.Client.TokenID = vmHandler.CredentialInfo.AuthToken
	authHeader := vmHandler.Client.AuthenticatedHeaders()

	requestOpts := client.RequestOpts{
		MoreHeaders: authHeader,
	}

	// VM 정보 조회
	vmInfo, err := vmHandler.GetVM(vmNameID)
	if err != nil {
		cblogger.Error(err)
		return irs.Failed, err
	}

	// 연결된 PublicIP 반환
	if vmInfo.PublicIP != "" {

		/*reqOpts := irs.PublicIPReqInfo{
			Name: vmInfo.PublicIP,
		}*/
		if ok, err := vmHandler.DisassociatePublicIP( /*reqOpts*/ vmInfo.PublicIP); !ok {
			return irs.Failed, err
		}

		time.Sleep(5 * time.Second)
	}

	if err := server.Terminate(vmHandler.Client, vmInfo.IId.NameId, &requestOpts); err != nil {
		cblogger.Error(err)
		panic(err)
		return irs.Failed, err
	}

	// VM 상태 정보 반환
	return irs.Terminating, nil
}

func (vmHandler *ClouditVMHandler) ListVMStatus() ([]*irs.VMStatusInfo, error) {
	vmHandler.Client.TokenID = vmHandler.CredentialInfo.AuthToken
	authHeader := vmHandler.Client.AuthenticatedHeaders()

	requestOpts := client.RequestOpts{
		MoreHeaders: authHeader,
	}

	if vmList, err := server.List(vmHandler.Client, &requestOpts); err != nil {
		cblogger.Error(err)
		return []*irs.VMStatusInfo{}, err
	} else {
		var vmStatusList []*irs.VMStatusInfo
		for _, vm := range *vmList {
			vmStatusInfo := irs.VMStatusInfo{
				IId: irs.IID{
					NameId: vm.ID,
				},
				VmStatus: irs.VMStatus(vm.State),
			}
			vmStatusList = append(vmStatusList, &vmStatusInfo)
		}
		return vmStatusList, nil
	}
}

func (vmHandler *ClouditVMHandler) GetVMStatus(vmNameID irs.IID) (irs.VMStatus, error) {
	vmHandler.Client.TokenID = vmHandler.CredentialInfo.AuthToken
	authHeader := vmHandler.Client.AuthenticatedHeaders()

	requestOpts := client.RequestOpts{
		MoreHeaders: authHeader,
	}

	vmID, err := vmHandler.getVmIdByName(vmNameID.NameId)
	if err != nil {
		cblogger.Error(err)
		return irs.Failed, err
	}

	vm, err := server.Get(vmHandler.Client, vmID, &requestOpts)
	if err != nil {
		cblogger.Error(err)
		return irs.Failed, err
	}

	// Set VM Status Info
	var resultStatus string
	switch strings.ToLower(vm.State) {
	case "creating":
		resultStatus = "Creating"
	case "running":
		resultStatus = "Running"
	case "stopping":
		resultStatus = "Suspending"
	case "stopped":
		resultStatus = "Suspended"
	case "starting":
		resultStatus = "Resuming"
	case "rebooting":
		resultStatus = "Rebooting"
	case "terminating":
		resultStatus = "Terminating"
	case "terminated":
		resultStatus = "Terminated"
	case "failed":
	default:
		resultStatus = "Failed"
	}
	return irs.VMStatus(resultStatus), nil
}

func (vmHandler *ClouditVMHandler) ListVM() ([]*irs.VMInfo, error) {
	vmHandler.Client.TokenID = vmHandler.CredentialInfo.AuthToken
	authHeader := vmHandler.Client.AuthenticatedHeaders()

	requestOpts := client.RequestOpts{
		MoreHeaders: authHeader,
	}

	if vmList, err := server.List(vmHandler.Client, &requestOpts); err != nil {
		cblogger.Error(err)
		return []*irs.VMInfo{}, err
	} else {
		var vmInfoList []*irs.VMInfo
		for _, vm := range *vmList {
			vmInfo := mappingServerInfo(vm)
			vmInfoList = append(vmInfoList, &vmInfo)
		}
		return vmInfoList, nil
	}
}

func (vmHandler *ClouditVMHandler) GetVM(vmNameID irs.IID) (irs.VMInfo, error) {
	vmHandler.Client.TokenID = vmHandler.CredentialInfo.AuthToken
	authHeader := vmHandler.Client.AuthenticatedHeaders()

	requestOpts := client.RequestOpts{
		MoreHeaders: authHeader,
	}

	// VM 이름으로 ID 정보 가져오기
	vmID, err := vmHandler.getVmIdByName(vmNameID.NameId)
	if err != nil {
		err := errors.New(fmt.Sprintf("failed to find vm with name %s", vmNameID.NameId))
		return irs.VMInfo{}, err
	}

	if vm, err := server.Get(vmHandler.Client, vmID, &requestOpts); err != nil {
		cblogger.Error(err)
		return irs.VMInfo{}, err
	} else {
		vmInfo := mappingServerInfo(*vm)
		return vmInfo, nil
	}
}

// VM에 PublicIP 연결
func (vmHandler *ClouditVMHandler) AssociatePublicIP(vmName string, vmIp string) (bool, error) {
	vmHandler.Client.TokenID = vmHandler.CredentialInfo.AuthToken
	authHeader := vmHandler.Client.AuthenticatedHeaders()

	var availableIP adaptiveip.IPInfo

	// 1. 사용 가능한 PublicIP 목록 가져오기
	requestOpts := client.RequestOpts{
		MoreHeaders: authHeader,
	}
	if availableIPList, err := adaptiveip.ListAvailableIP(vmHandler.Client, &requestOpts); err != nil {
		return false, err
	} else {
		if len(*availableIPList) == 0 {
			allocateErr := errors.New(fmt.Sprintf("There is no PublicIPs to allocate"))
			return false, allocateErr
		} else {
			availableIP = (*availableIPList)[0]
		}
	}

	// 2. PublicIP 생성 및 할당
	reqInfo := adaptiveip.PublicIPReqInfo{
		IP:        availableIP.IP,
		Name:      vmName + "-PublicIP",
		PrivateIP: vmIp,
	}

	createOpts := client.RequestOpts{
		JSONBody:    reqInfo,
		MoreHeaders: authHeader,
	}
	_, err := adaptiveip.Create(vmHandler.Client, &createOpts)
	if err != nil {
		cblogger.Error(err)
		return false, err
	} else {
		return true, nil
	}
}

// VM에 PublicIP 해제
func (vmHandler *ClouditVMHandler) DisassociatePublicIP(vmName string) (bool, error) {
	vmHandler.Client.TokenID = vmHandler.CredentialInfo.AuthToken
	authHeader := vmHandler.Client.AuthenticatedHeaders()

	requestOpts := client.RequestOpts{
		MoreHeaders: authHeader,
	}

	if err := adaptiveip.Delete(vmHandler.Client, vmName, &requestOpts); err != nil {
		cblogger.Error(err)
		return false, err
	} else {
		return true, nil
	}
}

func mappingServerInfo(server server.ServerInfo) irs.VMInfo {

	// Get Default VM Info
	vmInfo := irs.VMInfo{
		IId: irs.IID{
			NameId:   server.Name,
			SystemId: server.ID,
		},
		ImageIId: irs.IID{
			NameId: server.TemplateID,
		},
		VMSpecName: server.SpecId,
		// VirtualNetworkId: server.SubnetAddr,
		PublicIP:  server.AdaptiveIp,
		PrivateIP: server.PrivateIp,
		//KeyPairIId:       server.RootPassword,
	}

	/*if creatTime, err := time.Parse(time.RFC3339, server.CreatedAt); err == nil {
		vmInfo.StartTime = creatTime
	} else {
		panic(err)
	}*/

	return vmInfo
}

func (vmHandler *ClouditVMHandler) getVmIdByName(vmNameID string) (string, error) {
	var vmId string

	// VM 목록 검색
	vmList, err := vmHandler.ListVM()
	if err != nil {
		return "", err
	}

	// VM 목록에서 Name 기준 검색
	for _, v := range vmList {
		if strings.EqualFold(v.IId.NameId, vmNameID) {
			vmId = v.IId.NameId
			break
		}
	}

	// 만약 VM이 검색되지 않을 경우 에러 처리
	if vmId == "" {
		err := errors.New(fmt.Sprintf("failed to find vm with name %s", vmNameID))
		return "", err
	}
	return vmId, nil
}
