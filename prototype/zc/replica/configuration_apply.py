#!/usr/bin/env python3
# -*- coding: utf-8 -*-
import json
from zc.common import *

class ReplicaConfigurationApplyMixin:
    def applyConfigurationImportData(self, importData, templateTotal):
        '''
        configuration.importを実行する
        '''
        process = 'Template Import'
        templateResult = {'total': templateTotal, 'success': 0, 'failed': 0, 'messages': []}
        PRINT_PROG(f'{TAB*2}{process}:', self.CONFIG.quiet)
        for importItems in importData:
            if self.templateSkip and importItems.get('templates'):
                continue
            result = self.applyConfigurationImportItem(importItems, templateResult, process)
            if not result[0]:
                return result

        # テンプレートインポートの結果
        if self.templateSkip:
            PRINT_PROG(f'\r{TAB*2}', self.CONFIG.quiet)
            self.LOGGER.info(f'{process}: SKIP.')
        else:
            res = self.formatTemplateImportResult(templateResult)
            PRINT_PROG(f'\r{TAB*2}', self.CONFIG.quiet)
            self.LOGGER.info(f'{process}: {res}')
            for message in  templateResult['messages']:
                PRINT_PROG(f'\r{TAB*3}', self.CONFIG.quiet)
                self.LOGGER.error(f'Import Error[{message["name"]}]: {message["error"]}')

        return ZC_COMPLETE

    def applyConfigurationImportItem(self, importItems, templateResult, process):
        # テンプレート用処理
        if 'templates' in importItems.keys():
            # 処理するテンプレートの名前
            templateProcess = importItems['templates'][0]['name']
        else:
            templateProcess = None
        importItems.update(
            {
                'version': str(self.getLatestVersion('MASTER_VERSION')),
                'date': ZABBIX_TIME()
            }
        )
        # 7.0対応
        if self.getLatestVersion('MASTER_VERSION') >= 7.0:
            importItems.pop('date', None)
        # インポート内容のJSONテキスト化
        try:
            importItems = '{"zabbix_export":%s}' % json.dumps(importItems, ensure_ascii=False)
        except:
            return (False, f'Failed Convert ImportFile: {self.getLatestVersion("VERSION_ID")}')
        sections = sorted(json.loads(importItems)['zabbix_export'].keys())
        self.LOGGER.info(f'{process}: Execute Import target={templateProcess if templateProcess else "base"} sections={sections}')
        # インポート実行
        try:
            result = self.ZAPI.configuration.import_(
                **{
                    'format': 'json',
                    'rules': self.importRules,
                    'source': importItems,
                }
            )
            # テンプレートを処理している場合の結果
            if templateProcess:
                if not result:
                    templateResult['failed'] += 1
                    templateResult['message'].append(
                        {
                            'name': templateProcess,
                            'error': 'No Result return.'
                        }
                    )
                else:
                    templateResult['success'] += 1
        except Exception as e:
            if templateProcess:
                templateResult['failed'] += 1
                templateResult['messages'].append(
                    {
                        'name': templateProcess,
                        'error': e
                    }
                )
            else:
                # テンプレート以外の失敗は即終了
                PRINT_PROG(f'\r{TAB*2}', self.CONFIG.quiet)
                self.LOGGER.error(f'{process}: Failed.')
                return (False, f'Failed Execute Import.\n{e}')

        if templateProcess:
            res = self.formatTemplateImportResult(templateResult)
            PRINT_PROG(f'\r{TAB*2}{process}: {res}', self.CONFIG.quiet)
        return ZC_COMPLETE

    def formatTemplateImportResult(self, templateResult):
        sum = templateResult['success'] + templateResult['failed']
        return f'{sum}/{templateResult["total"]} (success:{templateResult["success"]}/failed:{templateResult["failed"]})'
