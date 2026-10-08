using System.Text.Json;
using Courier.Contract;
using Xunit;

namespace Courier.ContractTests;

/// <summary>信封 DTO wire key 对齐断言(primitives.md Frozen v1)。
/// 序列化键以契约为准;测试用 System.Text.Json camelCase 策略往返验证。</summary>
public class EnvelopeTests
{
    static readonly JsonSerializerOptions Camel = new() { PropertyNamingPolicy = JsonNamingPolicy.CamelCase };

    const string FailureJson =
        "{\"error\":{\"code\":\"AUTH_TOKEN_EXPIRED\",\"message\":\"session expired\",\"retryable\":false}," +
        "\"traceId\":\"4bf92f3577b34da6a3ce929d0e0e4736\"}";

    [Fact]
    public void FailureEnvelopeRoundTrip()
    {
        var env = JsonSerializer.Deserialize<ErrorEnvelope>(FailureJson, Camel);
        Assert.NotNull(env);
        Assert.Equal("AUTH_TOKEN_EXPIRED", env.Error.Code);
        Assert.Equal("session expired", env.Error.Message);
        Assert.False(env.Error.Retryable);
        Assert.Equal(32, env.TraceId.Length);

        var back = JsonSerializer.Serialize(env, Camel);
        Assert.Contains("\"code\"", back);
        Assert.Contains("\"traceId\"", back);
        Assert.Contains("\"retryable\":false", back);
        Assert.DoesNotContain("\"data\"", back);  // 失败信封无 data,互斥
    }

    [Fact]
    public void SuccessEnvelopeHasDataOnly()
    {
        var env = JsonSerializer.Deserialize<SuccessEnvelope<JsonElement>>("{\"data\":{\"k\":1}}", Camel);
        Assert.Equal(JsonValueKind.Object, env.Data.ValueKind);

        var back = JsonSerializer.Serialize(env, Camel);
        Assert.Contains("\"data\"", back);
        Assert.DoesNotContain("\"traceId\"", back);  // 成功响应不携带 traceId
    }

    [Fact]
    public void PageRoundTrip()
    {
        var page = JsonSerializer.Deserialize<Page<JsonElement>>("{\"items\":[],\"nextCursor\":\"\"}", Camel);
        Assert.Empty(page.Items);
        Assert.Equal("", page.NextCursor);  // 空串 = 末页

        // nextCursor 缺失 = 末页(可选字段容忍)。
        var noCursor = JsonSerializer.Deserialize<Page<JsonElement>>("{\"items\":[]}", Camel);
        Assert.Null(noCursor.NextCursor);
    }

    [Fact]
    public void WireKeyConstsMatchContract()
    {
        Assert.Equal("code", ApiError.KeyCode);
        Assert.Equal("message", ApiError.KeyMessage);
        Assert.Equal("retryable", ApiError.KeyRetryable);
        Assert.Equal("error", ErrorEnvelope.KeyError);
        Assert.Equal("traceId", ErrorEnvelope.KeyTraceId);
        Assert.Equal("data", SuccessEnvelope<int>.KeyData);
        Assert.Equal("items", Page<int>.KeyItems);
        Assert.Equal("nextCursor", Page<int>.KeyNextCursor);
    }
}
