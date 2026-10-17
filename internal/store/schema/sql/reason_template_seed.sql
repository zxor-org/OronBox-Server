
INSERT INTO moderation_reason_templates(id,scope,decision,title,body,position) VALUES
 (md5('tpl-resource-assets')::uuid,'resource_review','reject','素材不完整','截图或图标缺失、模糊，或与实际界面不一致，请补充清晰的预览素材后重新提交。',10),
 (md5('tpl-resource-metadata')::uuid,'resource_review','reject','信息填写不规范','名称、简介或分类与资源实际内容不符，请按规范修改后重新提交。',20),
 (md5('tpl-resource-package')::uuid,'resource_review','reject','安装包无法验证','安装包无法在目标设备上正常安装或运行，请自测通过后重新提交。',30),
 (md5('tpl-resource-copyright')::uuid,'resource_review','reject','版权与授权存疑','未能确认素材或代码的授权来源，请补充授权说明。',40),
 (md5('tpl-comment-abuse')::uuid,'comment','hide','人身攻击或辱骂','评论包含攻击性言论，已按社区规范隐藏。',10),
 (md5('tpl-comment-spam')::uuid,'comment','hide','垃圾信息或引流','评论包含广告、外链引流或重复刷屏内容。',20),
 (md5('tpl-plugin-permission')::uuid,'plugin','reject','权限申请超出功能需要','插件申请的权限明显超出其描述的功能范围，请精简后重新提交。',10),
 (md5('tpl-collection-scope')::uuid,'collection','reject','合集内容不成主题','合集内资源缺乏一致主题，请调整后重新提交。',10),
 (md5('tpl-report-nonissue')::uuid,'report','dismiss','未发现违规','经核查未发现被举报内容违反社区规范。',10),
 (md5('tpl-report-handled')::uuid,'report','','已处理','举报属实，相关内容已按社区规范处理，感谢反馈。',20)
ON CONFLICT DO NOTHING;
