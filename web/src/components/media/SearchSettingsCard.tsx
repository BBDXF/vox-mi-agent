import { useEffect, useState } from 'react';
import { getSettingsSchema, type SchemaSection } from '@/lib/api';
import { GenericSectionForm } from '@/components/settings/GenericSectionForm';

export function SearchSettingsCard() {
  const [sec, setSec] = useState<SchemaSection | null>(null);
  useEffect(() => {
    getSettingsSchema().then((r) => {
      const s = r.sections?.find((x) => x.name === 'search');
      if (s) setSec({ ...s, title: '点歌检索' });
    }).catch(() => {});
  }, []);
  return sec ? <GenericSectionForm section={sec} /> : null;
}
