# 契约测试:Godot 端错误枚举/信封 key 与 fixture 对齐断言。
# 运行(装 Godot 4 后,从仓库根):
#   godot --headless --path sdks/godot --script tests/test_contract.gd
# 本机无 Godot 二进制时由 tools/contractgen 保证 fixture↔生成物一致。
extends SceneTree

const Errors := preload("res://contract/errors.gd")
const Envelope := preload("res://contract/envelope.gd")
const FIXTURE := "res://../../docs/contract/fixtures/errors.json"


func _init() -> void:
	var failed := _run()
	quit(1 if failed > 0 else 0)


func _run() -> int:
	var failed := 0
	var f := FileAccess.open(FIXTURE, FileAccess.READ)
	if f == null:
		print("FAIL: 打不开 fixture %s(请从仓库根运行)" % FIXTURE)
		return 1
	var data: Dictionary = JSON.parse_string(f.get_as_text())
	var codes: Array = data["codes"]

	# 每个冻结码都有 spec,且 http/retryable 与 fixture 一致。
	for c in codes:
		var spec: Dictionary = Errors.spec_of(c["code"])
		if spec.is_empty():
			print("FAIL: 缺码 " + str(c["code"]))
			failed += 1
			continue
		if spec["http"] != c["http"] or spec["retryable"] != c["retryable"]:
			print("FAIL: 映射不一致 " + str(c["code"]))
			failed += 1

	# 域前缀数量一致。
	if Errors.PREFIXES.size() != data["prefixes"].size():
		print("FAIL: 域前缀数量不一致")
		failed += 1

	# 未知 wire code 容忍(返回空)。
	if Errors.name_of("NOT_A_CODE") != "":
		print("FAIL: 未知码应返回空串")
		failed += 1

	# 信封 wire key(primitives.md Frozen v1)。
	if Envelope.KEY_DATA != "data" or Envelope.KEY_ERROR != "error" \
			or Envelope.KEY_TRACE_ID != "traceId" or Envelope.KEY_NEXT_CURSOR != "nextCursor":
		print("FAIL: 信封 key 漂移")
		failed += 1

	if failed == 0:
		print("godot contract tests: all green")
	else:
		print("%d failed" % failed)
	return failed
