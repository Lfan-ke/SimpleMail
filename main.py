import asyncio
from common import (
    ConfigLoader, ConsulKVClient, PulsarService, KVServiceMeta,
)
from logger import logger
from service import (
    create_mail_task, mail_field_description, EmailMessage
)

config = ConfigLoader()

async def mail_handler(payload: dict[str, ...]) -> bool:
    """邮件服务处理器"""
    try:
        mail = EmailMessage.from_dict(payload)
        task = create_mail_task(mail)
        return await task
    except Exception as e:
        await logger.error(f"💥 [mail] 处理异常: {e}")
        return False

async def main():
    logger.set_app_name("EchoWing Mail Service")

    mail_service = PulsarService(
        service_name=config.config.Name,
        pulsar_url=config.config.Pulsar.Url,
        main_topic=config.main_topic,
        dlq_topic=config.dlq_topic,
    )

    await mail_service.start(
        message_handler=mail_handler,
    )

    consul = ConsulKVClient(
        host=config.config.Consul.Host,
        port=config.config.Consul.Port,
        token=config.config.Consul.Token,
        scheme=config.config.Consul.Scheme,
        kv_base_path=config.config.Consul.Base,
    )

    schema = KVServiceMeta(
        ServerName=config.config.Name,
        ServerDesc="EchoWing 通用邮件服务",
        ServerIcon=None,
        ServerPath=config.config.Pulsar.Main,
        ServerData={"fields": {
            **mail_field_description
        }}
    )

    await consul.register_kv(config.config.Name, schema.to_dict())

    await logger.info(f"📧 已注册 KV 到 Consul ...")
    await logger.info("🎯 邮件服务已启动，配置了自动重试和死信队列")

    try:
        await asyncio.gather(mail_service.task)
    except asyncio.CancelledError:
        await logger.info("🛑 服务被终止")
    except Exception as e:
        await logger.error(f"💥 主程序异常: {e}")
    finally:
        await mail_service.stop()
        await consul.deregister_kv(config.config.Name)
        await logger.info(f"🚮 已注销 KV 从 Consul ...")

if __name__ == "__main__":
    asyncio.run(main())
