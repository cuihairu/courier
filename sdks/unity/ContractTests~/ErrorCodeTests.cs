using System;
using System.Collections.Generic;
using System.IO;
using System.Text.Json;
using Courier.Contract;
using Xunit;

namespace Courier.ContractTests;

/// <summary>错误枚举与契约 fixture 对齐断言(docs/contract/fixtures/errors.json)。</summary>
public class ErrorCodeTests
{
    static readonly string FixturePath = FindRepoFile(Path.Combine("docs", "contract", "fixtures", "errors.json"));

    static JsonElement LoadFixture()
    {
        var doc = JsonDocument.Parse(File.ReadAllText(FixturePath));
        return doc.RootElement.Clone();
    }

    // 从测试输出目录向上找仓库根(含 docs/contract/fixtures 的层级)。
    static string FindRepoFile(string rel)
    {
        var dir = new DirectoryInfo(AppContext.BaseDirectory);
        while (dir != null)
        {
            var candidate = Path.Combine(dir.FullName, rel);
            if (File.Exists(candidate)) return candidate;
            dir = dir.Parent;
        }
        throw new FileNotFoundException("找不到契约 fixture(请从仓库内运行): " + rel);
    }

    [Fact]
    public void FrozenCodeCountMatchesFixture()
    {
        var fixture = LoadFixture();
        var codes = fixture.GetProperty("codes");
        Assert.Equal(codes.GetArrayLength(), ErrorSpecs.All.Count);
    }

    [Fact]
    public void AllFrozenCodesParseAndMatchSpec()
    {
        var codes = LoadFixture().GetProperty("codes");
        foreach (var row in codes.EnumerateArray())
        {
            var wire = row.GetProperty("code").GetString();
            Assert.True(ErrorSpecs.TryParse(wire, out var code), "缺码 " + wire);
            Assert.True(ErrorSpecs.TryGet(code, out var spec));
            Assert.Equal(wire, spec.Wire);
            Assert.Equal(row.GetProperty("http").GetInt32(), spec.Http);
            Assert.Equal(row.GetProperty("retryable").GetBoolean(), spec.Retryable);
        }
    }

    [Fact]
    public void UnknownWireCodeIsTolerated()
    {
        // 契约 versioning.md:未知 code 必须容忍,落兜底分支。
        Assert.False(ErrorSpecs.TryParse("NOT_A_CODE", out _));
    }

    [Fact]
    public void PrefixRegistryMatchesFixture()
    {
        var prefixes = LoadFixture().GetProperty("prefixes");
        var expected = new List<string>();
        foreach (var p in prefixes.EnumerateArray()) expected.Add(p.GetString());
        Assert.Equal(expected, ErrorSpecs.Prefixes);
    }
}
