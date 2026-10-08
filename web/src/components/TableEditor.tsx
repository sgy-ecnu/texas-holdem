import { useCallback, useEffect, useState } from 'react'
import { ApiError, api } from '../api'
import type { HealthDTO, ModelDTO, TableDTO, TableConfigInput } from '../types'
import { GameConfigEditor } from './GameConfigEditor'

// 模板编辑页：创建/编辑模板并提交到 /tables。
// 编辑使用 PUT 全量替换 + expected_revision 做 CAS；版本冲突 409 提示刷新。
// 保存模板不要求真人会话：真人座位只保存「哪个位置是真人」。

interface Props {
  // null 表示新建模板。
  tableId: string | null
  models: ModelDTO[]
  health: HealthDTO | null
  healthOk: boolean
  onSaved: (table: TableDTO) => void
  onCancel: () => void
}

export function TableEditor({ tableId, models, health, healthOk, onSaved, onCancel }: Props) {
  const [table, setTable] = useState<TableDTO | null>(null)
  const [name, setName] = useState('')
  const [loading, setLoading] = useState(tableId !== null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [conflict, setConflict] = useState(false)
  // 每次成功加载/刷新递增，作为编辑器 key，确保表单用最新 revision 的配置重建。
  const [reloadToken, setReloadToken] = useState(0)

  const load = useCallback(async () => {
    if (tableId === null) {
      setTable(null)
      setLoading(false)
      return
    }
    setLoading(true)
    setLoadError(null)
    try {
      const dto = await api.table(tableId)
      setTable(dto)
      setName(dto.name)
      setReloadToken((n) => n + 1)
    } catch (err) {
      setLoadError(err instanceof ApiError ? err.message : '加载模板失败，请稍后重试。')
    } finally {
      setLoading(false)
    }
  }, [tableId])

  useEffect(() => {
    void load()
  }, [load])

  async function submit(config: TableConfigInput) {
    const trimmed = name.trim()
    if (!trimmed) {
      setError('模板名称不能为空。')
      return
    }
    setSubmitting(true)
    setError(null)
    setConflict(false)
    try {
      if (tableId === null) {
        const created = await api.createTable({ name: trimmed, config })
        onSaved(created)
      } else {
        if (!table) {
          setError('模板尚未加载完成，请稍后重试。')
          return
        }
        const updated = await api.updateTable(tableId, {
          expected_revision: table.revision,
          name: trimmed,
          config,
        })
        onSaved(updated)
      }
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setConflict(true)
        setError('模板已被其他会话修改（版本冲突），请刷新后重试。')
      } else {
        setError(err instanceof ApiError ? err.message : '保存模板失败，请稍后重试。')
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="container">
      {!healthOk && (
        <div className="banner warn">
          <span>
            <span className="banner-title">存储状态异常：</span>
            后端报告存储降级或不可用，模板可能无法保存或读取。
          </span>
        </div>
      )}

      <div className="panel panel-pad" style={{ marginBottom: 18 }}>
        <div className="field">
          <label>模板名称</label>
          <input
            className="input"
            value={name}
            maxLength={128}
            aria-label="模板名称"
            placeholder="例如：稳健 6 人桌"
            onChange={(e) => setName(e.target.value)}
          />
        </div>
      </div>

      {loading ? (
        <div className="splash">
          <span>
            <span className="spinner" /> 正在加载模板…
          </span>
        </div>
      ) : loadError ? (
        <div className="panel panel-pad">
          <div className="banner error" role="alert">{loadError}</div>
          <div className="form-row">
            <button type="button" className="btn btn-sm" onClick={() => void load()}>
              重试
            </button>
            <button type="button" className="btn btn-ghost btn-sm" onClick={onCancel}>
              返回列表
            </button>
          </div>
        </div>
      ) : (
        <>
          {conflict && (
            <div className="banner warn" role="alert">
              <span>
                <span className="banner-title">版本冲突：</span>
                模板已在别处被修改。请刷新载入最新版本后再保存，避免覆盖他人改动。
              </span>
              <button type="button" className="btn btn-sm" onClick={() => void load()}>
                刷新
              </button>
            </div>
          )}
          <GameConfigEditor
            key={`${tableId ?? 'new'}:${reloadToken}`}
            title={tableId === null ? '新建模板' : '编辑模板'}
            hint={
              tableId === null
                ? '保存一份可复用的牌桌配置；真人座位只记录位置，开局时由打开浏览器持有权限。'
                : '编辑使用全量替换并做版本校验；保存后已开始的牌局不受影响。'
            }
            models={models}
            health={health}
            initial={table?.config ?? null}
            submitLabel={tableId === null ? '创建模板' : '保存修改'}
            submitting={submitting}
            remoteError={error}
            onSubmit={submit}
            onCancel={onCancel}
          />
        </>
      )}
    </div>
  )
}
