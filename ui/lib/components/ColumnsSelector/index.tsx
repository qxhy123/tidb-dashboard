import React, { ReactNode, useMemo, useState, useEffect } from 'react'
import { Checkbox, Popover, Space, Button } from 'antd'
import { DownOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { IColumn } from 'office-ui-fabric-react/lib/DetailsList'
import { addTranslationResource } from '@lib/utils/i18n'

import styles from './index.module.less'

const translations = {
  en: {
    trigger_text: 'Columns',
    select: 'Select',
    reset: 'Reset',
  },
  'zh-CN': {
    trigger_text: '选择列',
    select: '选择',
    reset: '重置',
  },
}

for (const key in translations) {
  addTranslationResource(key, {
    component: {
      columnsSelector: translations[key],
    },
  })
}

export interface IColumnKeys {
  [key: string]: boolean
}

export interface IColumnsSelectorProps {
  columns: IColumn[]
  visibleColumnKeys?: IColumnKeys
  resetColumnKeys?: IColumnKeys
  onChange?: (visibleKeys: IColumnKeys) => void
  foot?: ReactNode
}

export default function ColumnsSelector({
  columns,
  visibleColumnKeys,
  resetColumnKeys,
  onChange,
  foot,
}: IColumnsSelectorProps) {
  const { t } = useTranslation()
  const [indeterminate, setIndeterminate] = useState(true)
  const [checkedAll, setCheckedAll] = useState(false)

  const filteredColumns = useMemo(
    () => columns.filter((c) => c.key !== 'dummy'),
    [columns]
  )

  const visibleKeys = useMemo(() => {
    if (visibleColumnKeys) {
      return visibleColumnKeys
    }
    return columns.reduce((acc, cur) => {
      acc[cur.key] = true
      return acc
    }, {})
  }, [visibleColumnKeys, columns])

  useEffect(() => {
    function updateCheckAllStatus(columnKeys) {
      const checkedKeysCount = Object.keys(columnKeys).filter(
        (k) => columnKeys[k] && k !== 'dummy'
      ).length
      setIndeterminate(
        checkedKeysCount > 0 && checkedKeysCount < filteredColumns.length
      )
      setCheckedAll(checkedKeysCount === filteredColumns.length)
    }

    updateCheckAllStatus(visibleKeys)
  }, [visibleKeys, filteredColumns])

  function handleCheckAllChange(e) {
    const checked = e.target.checked
    const newVisibleKeys = columns.reduce((acc, cur) => {
      acc[cur.key] = checked
      return acc
    }, {})
    onChange && onChange(newVisibleKeys)
  }

  function handleCheckChange(e, column: IColumn) {
    const checked = e.target.checked
    const newVisibleKeys = {
      ...visibleKeys,
      [column.key]: checked,
    }
    onChange && onChange(newVisibleKeys)
  }

  const title = (
    <div className={styles.title_container}>
      <Checkbox
        indeterminate={indeterminate}
        checked={checkedAll}
        onChange={handleCheckAllChange}
      >
        {t('component.columnsSelector.select')}
      </Checkbox>
      {resetColumnKeys && (
        <Button
          type="link"
          onClick={() => onChange && onChange(resetColumnKeys)}
        >
          {t('component.columnsSelector.reset')}
        </Button>
      )}
    </div>
  )

  const content = (
    <Space direction="vertical">
      {filteredColumns.map((column) => (
        <Checkbox
          key={column.key}
          checked={visibleKeys[column.key]}
          onChange={(e) => handleCheckChange(e, column)}
        >
          {column.name}
        </Checkbox>
      ))}

      {foot && <div className={styles.foot_container}>{foot}</div>}
    </Space>
  )

  return (
    <Popover content={content} title={title} placement="bottomLeft">
      <span style={{ cursor: 'pointer' }}>
        {t('component.columnsSelector.trigger_text')} <DownOutlined />
      </span>
    </Popover>
  )
}
