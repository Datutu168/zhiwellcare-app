/**
 * 课程训练 —— 本地 Mock 数据与类型。
 *
 * 内容定位：居家上肢健身 / 日常机能维持锻炼 / 主动发力训练。
 * 合规基线：全部课程为纯健身消费产品内容，仅提供通用居家健身指导，
 * 不含任何健康专业表述；无视频资源，均为图文跟练。
 *
 * 取数方式：本文件是后端内容接口不可用时的兜底数据源
 * （由 src/core/content/LocalContentSource.ts 读取并归一化）；
 * 页面取数请统一走 ContentService，不要直接 import 本文件。
 */

/** 课程分类（不包含筛选用「全部」）。 */
export type CourseCategory = '上肢' | '手腕' | '手指' | '协调' | '放松'
/** 课程难度。 */
export type CourseLevel = '入门' | '进阶'

/** 单节动作：title 动作名，detail 动作要领说明。 */
export interface CourseStep {
  title: string
  detail: string
}

export interface Course {
  id: string
  title: string
  summary: string
  category: CourseCategory
  level: CourseLevel
  /** 预计时长（分钟）。 */
  durationMin: number
  tags: string[]
  /** 封面占位 Emoji。 */
  cover: string
  steps: CourseStep[]
  /** 可选：本节专属运动提示（纯健身范畴）。 */
  disclaimer?: string
}

/** 首页分类筛选 chips，首项为「全部」。 */
export const courseCategories: string[] = ['全部', '上肢', '手腕', '手指', '协调', '放松']

export const courses: Course[] = [
  {
    id: 'shoulder-circles-warmup',
    title: '肩部环绕热身',
    summary: '居家上肢健身开篇热身：缓慢环绕、放松肩臂，为后续跟练做好准备。',
    category: '上肢',
    level: '入门',
    durationMin: 8,
    tags: ['零器械', '晨间适合', '唤醒肩臂'],
    cover: '🌀',
    steps: [
      { title: '站姿准备', detail: '双脚与肩同宽自然站立，腰背挺直，双肩放松下沉，目视前方，呼吸平稳。' },
      { title: '慢速前绕', detail: '双肩向前缓慢画圆，每圈约 3 秒，完成 8 圈，幅度由小到大。' },
      { title: '慢速后绕', detail: '双肩向后缓慢画圆 8 圈，保持均匀节奏，不要耸肩、不要憋气。' },
      { title: '交替耸放', detail: '双肩交替向上提起再缓慢放下，每侧 6 次，动作放松连贯。' },
      { title: '收尾放松', detail: '双臂自然垂放，轻轻甩动肩膀 20 秒，感觉肩臂舒展即可结束。' },
    ],
    disclaimer: '绕动过程中请保持匀速，若肩部明显不适可缩小幅度或暂停本节。',
  },
  {
    id: 'wrist-mobility-routine',
    title: '手腕灵活动作操',
    summary: '桌面即练的腕部活动操：前后摆动、左右翻转与绕环，让手腕保持灵活好状态。',
    category: '手腕',
    level: '入门',
    durationMin: 6,
    tags: ['久坐友好', '办公间隙', '零器械'],
    cover: '🤲',
    steps: [
      { title: '预备坐姿', detail: '坐直后前臂平放桌面，手腕自然伸出桌沿，双手放松握空拳。' },
      { title: '前后屈伸', detail: '手腕缓慢向前上方翘起，再缓慢向下压，两端各停顿 1 秒，完成 10 次。' },
      { title: '左右摆动', detail: '手腕向左、向右缓慢摆动，幅度以舒服为准，两端各停 1 秒，完成 10 次。' },
      { title: '绕环画圈', detail: '双手握空拳，手腕同时顺时针绕环 8 圈，再逆时针绕环 8 圈，动作放慢。' },
      { title: '甩腕收尾', detail: '双手自然垂在体侧，轻轻甩动手腕与手指 15 秒，让手腕完全放松。' },
    ],
    disclaimer: '全程动作轻柔缓慢，桌面高度以手臂能放松放平为宜。',
  },
  {
    id: 'forearm-stretch-relax',
    title: '前臂拉伸放松',
    summary: '练后与久坐后的前臂放松：通过轻柔拉伸与旋转，让紧绷的手臂肌肉松一口气。',
    category: '放松',
    level: '入门',
    durationMin: 10,
    tags: ['拉伸放松', '久坐后', '零器械'],
    cover: '🧘',
    steps: [
      { title: '站姿调息', detail: '自然站立或盘坐，双臂放松垂放，先做 3 次缓慢深呼吸，让身体安定下来。' },
      { title: '掌心上抬拉伸', detail: '右臂前伸掌心向上，左手轻扶右手手指向下轻压，保持 20 秒后换边。' },
      { title: '掌心下压拉伸', detail: '右臂前伸掌心向下，左手轻握右手手背向下轻压，保持 20 秒后换边。' },
      { title: '内外旋活动', detail: '前臂弯曲约 90 度，手掌缓慢向内、向外翻转各 10 次，速度放慢。' },
      { title: '双臂环绕收尾', detail: '双臂前后各慢速画大圈 5 次，再自然甩动双臂 15 秒结束。' },
    ],
    disclaimer: '拉伸以轻微牵拉感为准，不要用力按压或快速弹动。',
  },
  {
    id: 'finger-grip-release',
    title: '手指抓放练习',
    summary: '零器械手指练习：张开、抓握与指尖对捏，适合用眼用手的日常间隙来一段。',
    category: '手指',
    level: '入门',
    durationMin: 8,
    tags: ['抓握练习', '日常间隙', '零器械'],
    cover: '✊',
    steps: [
      { title: '手指开合', detail: '双手举于胸前，五指尽量张开保持 2 秒，再慢慢握成空拳，重复 15 次。' },
      { title: '指尖对捏', detail: '拇指依次与食指、中指、无名指、小指轻轻对捏，每对 5 次，双手同时进行。' },
      { title: '抓握练习', detail: '可用毛巾或软球辅助抓握，握紧 3 秒后慢慢松开，每只手 10 次；徒手空握亦可。' },
      { title: '弹指轻点', detail: '手指像弹琴一样在桌面依次轻点，从慢到快各做 20 秒，保持放松不紧绷。' },
      { title: '互揉放松', detail: '双手手指交叉，轻轻揉搓掌心与指根 30 秒，完成本节放松。' },
    ],
  },
  {
    id: 'baduanjin-style-upper-stretch',
    title: '八段锦风格上肢伸展',
    summary: '融合传统导引动作的上肢伸展：托举、开弓、绕摆一气呵成，找回舒展挺拔的姿态。',
    category: '上肢',
    level: '入门',
    durationMin: 12,
    tags: ['传统导引风格', '舒展', '零器械'],
    cover: '🌿',
    steps: [
      { title: '起势调息', detail: '双脚开立与肩同宽，双手自体前缓慢上举过头，再翻掌下按，配合呼吸做 3 遍。' },
      { title: '托天伸展', detail: '双手在胸前交叉，缓慢上托至头顶上方，目视手背，保持 5 秒后缓缓下落，做 5 遍。' },
      { title: '左右开弓', detail: '身体保持中正，双手模拟拉弓向左右交替推出，各 6 次，动作舒展有力。' },
      { title: '单臂侧展', detail: '左手上举贴耳，身体向右侧缓慢侧屈，右手沿腿侧下滑，保持 8 秒后换边，各 3 次。' },
      { title: '摆手收势', detail: '双手自然垂放，随呼吸前后摆动双臂 8 次，最后安静站立 10 秒调匀呼吸。' },
    ],
  },
  {
    id: 'resistance-band-shoulder-back',
    title: '弹力带肩背主动训练',
    summary: '进阶弹力带课程：前平举、划船与推举组合，主动发力锻炼肩背与上臂。',
    category: '上肢',
    level: '进阶',
    durationMin: 15,
    tags: ['弹力带', '进阶', '肩背发力'],
    cover: '🎗️',
    steps: [
      { title: '检查与站位', detail: '练前检查弹力带无裂口，双脚踩稳弹力带中部，双手握紧两端，腰背挺直、微屈膝。' },
      { title: '前平举拉带', detail: '双手掌心向下握带，向前平举至与肩同高，停顿 1 秒后缓慢回落，做 10 次。' },
      { title: '俯身划船', detail: '屈髋俯身、背部挺直，双手向身体两侧拉带，感受肩胛向中间靠近，做 12 次。' },
      { title: '过头推举', detail: '弹力带踩于脚下，双手握带向上推举过头顶，手臂伸直后缓慢下放，做 10 次。' },
      { title: '侧向平举', detail: '双手握带与肩同宽，向身体两侧抬起至与肩同高，控制节奏缓慢放下，做 10 次。' },
      { title: '拉伸收尾', detail: '右臂横过胸前，左手轻扶右上臂向内轻拉 20 秒后换边，放松肩背结束。' },
    ],
    disclaimer: '弹力带阻力以能保持动作标准为度；全程缓慢控制，不要快速弹放弹力带。',
  },
  {
    id: 'finger-dexterity-advanced',
    title: '手指灵巧进阶操',
    summary: '进阶手指协调练习：桌面爬行、对指轮转与节奏轻点，让双手更灵巧更听话。',
    category: '手指',
    level: '进阶',
    durationMin: 10,
    tags: ['手指进阶', '桌面练习', '节奏'],
    cover: '🎹',
    steps: [
      { title: '桌面爬行', detail: '手指像蜘蛛一样在桌面依次向前“行走”，再倒走回来，每只手 3 轮，保持指节放松。' },
      { title: '对指轮转', detail: '拇指依次碰触食指、中指、无名指、小指，再反向回来，双手同做 3 轮，由慢到快。' },
      { title: '空中写字', detail: '用食指在空气中缓慢书写数字 1 到 10 和简单图案，左右手各写 1 遍。' },
      { title: '节奏轻点', detail: '双手手指交替在桌面轻点，先慢后快再慢下来，共 40 秒，保持手腕不紧绷。' },
      { title: '放松收尾', detail: '双手十指交叉，缓慢转动并轻揉手腕与指根 20 秒，结束本节。' },
    ],
  },
  {
    id: 'upper-limb-rhythm-coordination',
    title: '上肢协调节拍操',
    summary: '跟着节拍活动全身：交叉拍肩、镜像挥臂与踏步组合，锻炼左右配合与节奏感。',
    category: '协调',
    level: '进阶',
    durationMin: 14,
    tags: ['协调节拍', '上肢主导', '趣味'],
    cover: '🤹',
    steps: [
      { title: '节拍热身', detail: '原地小踏步 30 秒，同时双手交替轻拍对侧肩膀，先慢后快，找到自己的节拍。' },
      { title: '交叉拍打', detail: '右手拍左肩、左手拍右肩，同时交替轻抬对侧膝盖，完成 12 组，保持呼吸。' },
      { title: '镜像挥臂', detail: '双臂先同向上下挥动 8 次，再尝试反向镜像挥动 8 次，眼睛始终看前方。' },
      { title: '组合跟上', detail: '一拍踏左脚、双手前平举，二拍并步、双手放下，依此循环 8 组，跟着节拍完成。' },
      { title: '整理收尾', detail: '放慢踏步，双臂前后放松摆动 20 秒，再安静站立调匀呼吸结束。' },
    ],
  },
]
