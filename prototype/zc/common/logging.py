#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common.constants import *

def __LOGGER__(**params):
    # パラメーター処理
    logName = params.get('logName', __name__)
    logLevel = params.get('logLevel', DEFAULT_LOG_LEVEL).upper()
    if logLevel not in ['DEBUG', 'INFO', 'WARNING', 'ERROR', 'CRITICAL']:
        logLevel = DEFAULT_LOG_LEVEL
    logHanders = params.get('logHandlers', [DEFAULT_LOG_STREAM]) 
    # ロガー初期化
    logger = logging.getLogger(logName)
    for handler in logHanders:
        logger = __HANDLER__(
            logger,
            handler['handler'],
            logLevel,
            handler['format'],
            **handler.get('option', {})
        )
    logger.setLevel(getattr(logging, logLevel))
    logger.propagate = False
    return logger

def __HANDLER__(logger, handler, level, format, **option):
    # ハンドラー追加
    handler = getattr(logging, handler)(**option)
    handler.setLevel(level)
    handler.setFormatter(getattr(logging, 'Formatter')(fmt=format, datefmt=DEFAULT_LOG_DATE))
    logger.addHandler(handler)
    return logger

