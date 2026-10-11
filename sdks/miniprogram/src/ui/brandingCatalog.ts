// UI 包品牌目录(契约 branding.md:兜底规则落 UI——Core/Service 零解释)。
// 宿主在启动与 branding.updated 事件到达时调 applyBranding(热切换:version 相同即忽略);
// 面板经 tryGetString/get* 读当前品牌,任何字段缺失回落 Courier 内置默认标。
import type { BrandingDto } from "../service/appDto.ts";

/** Courier 默认标(契约「兜底规则」:整能力未接/字段缺失时的内置默认)。 */
export const BrandingDefaults = {
  companyName: "Courier",
  productTitle: "Courier",
  primaryColor: "#4C8DFF",
  supportLabel: "联系客服",
} as const;

let current: BrandingDto | null = null;
let version = -1;

/** 当前生效的品牌版本;-1 = 从未应用(全默认)。 */
export function currentBrandingVersion(): number {
  return version;
}

/** 应用品牌物料(热切换入口):null 或 version 相同即忽略。 */
export function applyBranding(branding: BrandingDto | null | undefined): void {
  if (branding == null || branding.version === version) {
    return;
  }
  current = branding;
  version = branding.version;
}

/** 读字符串字段(缺失返回 null)——消费方自行串兜底链用。 */
export function tryGetString(field: string): string | null {
  const value = current?.[field];
  return typeof value === "string" ? value : null;
}

/** 公司名(缺失 → Courier 默认)。 */
export function getCompanyName(): string {
  return tryGetString("companyName") ?? BrandingDefaults.companyName;
}

/** 产品名(缺失 → Courier 默认)。 */
export function getProductTitle(): string {
  return tryGetString("productName") ?? BrandingDefaults.productTitle;
}

/** 主题色(缺失 → Courier 默认;#RRGGBB;嵌套 theme.primaryColor)。 */
export function getPrimaryColor(): string {
  const theme = current?.["theme"];
  if (theme != null && typeof theme === "object") {
    const color = (theme as Record<string, unknown>)["primaryColor"];
    if (typeof color === "string" && color !== "") {
      return color;
    }
  }
  return BrandingDefaults.primaryColor;
}

/** 客服入口文案(缺失 → Courier 默认)。 */
export function getSupportLabel(): string {
  return tryGetString("supportEntryLabel") ?? BrandingDefaults.supportLabel;
}

/** 清空品牌(宿主重置/测试用;回到全默认态)。 */
export function resetBranding(): void {
  current = null;
  version = -1;
}
