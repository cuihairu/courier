# 服务层门面(unity/ts/ue CourierServices 同构):从 CourierClient 取 ApiClient 装配域服务。
# 依赖方向 service→core,core 不感知本层。
# GDScript 无异常:各域方法均为协程(await),返回 {ok:true, data} / {ok:true, data:null}
# (501 能力未接,TS null / UE nullopt 同构)/ {ok:false, error}(typed)。

class_name CourierServices

var announcements: CourierAnnouncementService
var support: CourierSupportService
var app: CourierAppService
var config: CourierAppConfigService
var branding: CourierBrandingService
var player: CourierPlayerService
var assistant: CourierAssistantService
var payments: CourierPaymentService
var realname: CourierRealNameService


func _init(client: CourierClient) -> void:
	announcements = CourierAnnouncementService.new(client.api)
	support = CourierSupportService.new(client.api)
	app = CourierAppService.new(client.api)
	config = CourierAppConfigService.new(client.api)
	branding = CourierBrandingService.new(client.api)
	player = CourierPlayerService.new(client.api)
	assistant = CourierAssistantService.new(client.api)
	payments = CourierPaymentService.new(client.api)
	realname = CourierRealNameService.new(client.api)
