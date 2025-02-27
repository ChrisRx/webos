package ssap

type Command string

const (
	APIGetServiceList                      Command = "ssap://api/getServiceList"
	ApplicationManagerGetForegroundAppInfo Command = "ssap://com.webos.applicationManager/getForegroundAppInfo"
	AudioGetVolume                         Command = "ssap://audio/getVolume"
	GetPointerInputSocket                  Command = "ssap://com.webos.service.networkinput/getPointerInputSocket"
	SendEnterKey                           Command = "ssap://com.webos.service.ime/sendEnterKey"
)
