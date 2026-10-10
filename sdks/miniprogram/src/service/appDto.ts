// M3 App/Config 域 DTO(契约 app.md、config.md、branding.md Frozen v1)。
// items/fields 为任意 JSON 值原样透传(SDK 不做二次校验——接入方结构变更是使用方错误)。

/** 远程配置快照:configVersion(热生效判据)+ 命中条件的键值全量。 */
export interface AppConfigDto {
  readonly configVersion: number;
  readonly items: Readonly<Record<string, unknown>>;
}

/** 品牌物料:version(热生效判据)+ 业务字段透传(SDK 零解释;消费全在接入方 UI)。 */
export interface BrandingDto {
  readonly version: number;
  readonly [key: string]: unknown;
}

/** 版本检查:forceUpdate = appVersion < minVersion(服务端判定)。 */
export interface AppVersionDto {
  readonly latestVersion: string;
  readonly minVersion: string;
  readonly updateUrl?: string; // 可选(payload 缺省即缺键,契约 app.md)
  readonly forceUpdate: boolean;
}

/** 维护状态:estimatedRecoveryAt/message 可选(缺省即缺键)。 */
export interface AppMaintenanceDto {
  readonly inMaintenance: boolean;
  readonly estimatedRecoveryAt?: string;
  readonly message?: string;
}

/** 环境回显:初始化校验(排查接错环境)。 */
export interface AppEnvironmentDto {
  readonly gameId: string;
  readonly env: string;
}
