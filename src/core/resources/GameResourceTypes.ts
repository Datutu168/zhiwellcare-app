/** CDN 游戏资源清单文件名（位于游戏资源目录前缀之下）。 */
export const GAME_RESOURCE_MANIFEST_FILE = 'manifest.json'

/**
 * 远程游戏资源定位信息：目录条目（GameCatalogEntry）中与 CDN 相关的字段子集。
 * 只依赖这三个字段，目录结构升级时本模块无需改动。
 */
export interface GameResourceLocator {
  gameId: string
  /** 内容资源版本（后端管控，参与缓存键与目录路径）。 */
  resourceVersion: string
  /** 该游戏该版本的资源目录前缀，如 https://cdn.example.com/games/kart-racing/1.0.0/ ；空串表示使用内置资源。 */
  resourceUrl: string
}

/** 清单中的单个文件条目（sha256/size 供完整性校验，可缺省）。 */
export interface GameResourceManifestFile {
  /** 相对资源目录前缀的路径，如 art/tile.png 。 */
  path: string
  sha256?: string
  size?: number
}

/** CDN 资源清单（{resourceUrl}manifest.json）：结构非法时整体降级为「无清单」。 */
export interface GameResourceManifest {
  version: string
  files: GameResourceManifestFile[]
}
