import asyncio
import base64
from dataclasses import dataclass, field
from email.message import EmailMessage
from email.mime.text import MIMEText
from email.mime.multipart import MIMEMultipart
from email.mime.base import MIMEBase
from email import encoders
import aiosmtplib
from datetime import datetime
from logger import logger


@dataclass
class EmailAttachment:
    """邮件附件"""
    filename: str
    content: str  # base64
    content_type: str = "application/octet-stream"

    @classmethod
    def from_dict(cls, data: dict) -> 'EmailAttachment':
        return cls(**data)

    @property
    def decoded_content(self) -> bytes:
        """解码base64内容"""
        return base64.b64decode(self.content)


@dataclass
class EmailMessage:
    to: list[str]
    subject: str
    content: str
    from_email: str | None = None
    cc: list[str] = field(default_factory=list)
    bcc: list[str] = field(default_factory=list)
    html: bool = False
    attachments: list[EmailAttachment] = field(default_factory=list)
    metadata: dict = field(default_factory=dict)

    @classmethod
    def from_dict(cls, json_data: dict) -> EmailMessage:
        data = json_data.copy()
        if 'attachments' in data:
            data['attachments'] = [EmailAttachment.from_dict(att) for att in data['attachments']]
        return cls(**{k: data[k] for k in data if k in cls.__annotations__})


def format_metadata_for_email(metadata: dict, is_html: bool = False) -> str:
    """
    格式化元数据用于邮件

    Args:
        metadata: 元数据字典
        is_html: 是否是HTML格式

    Returns:
        格式化后的元数据字符串
    """
    if not metadata:
        return ""

    # 处理特殊字段
    special_fields = ['user_id', 'app_id', 'function']
    special_lines = []

    for field in special_fields:
        if field in metadata:
            special_lines.append(f"{field}: {metadata[field]}")

    # 处理其他字段
    other_fields = {k: v for k, v in metadata.items() if k not in special_fields}

    if is_html:
        # HTML格式的处理
        html_lines = []

        if special_lines:
            # 创建HTML格式的元数据行
            html_special = []
            for i, line in enumerate(special_lines):
                html_special.append(f"<span style='color: #666; font-size: 12px;'>{line}</span>")
                if i < len(special_lines) - 1:
                    html_special.append(" <b>·</b> ")

            html_lines.append(f"<div style='margin-top: 20px; padding-top: 10px; border-top: 1px solid #eee; color: #999; font-size: 11px; line-height: 1.4;'>")
            html_lines.append(f"<div>{''.join(html_special)}</div>")

            if other_fields:
                # 将其他字段转换为字符串，转义HTML
                other_str = str(other_fields).replace('<', '&lt;').replace('>', '&gt;')
                html_lines.append(f"<div style='margin-top: 5px; color: #888; font-size: 10px;'>其他元数据: {other_str}</div>")

            html_lines.append("</div>")
            return "\n".join(html_lines)
        elif other_fields:
            # 只有其他字段
            other_str = str(other_fields).replace('<', '&lt;').replace('>', '&gt;')
            return f"<div style='margin-top: 20px; padding-top: 10px; border-top: 1px solid #eee; color: #999; font-size: 11px;'>其他元数据: {other_str}</div>"
    else:
        # 纯文本格式的处理
        text_lines = []

        if special_lines:
            # 使用短信类似的格式
            separator = " | "
            formatted_line = f"| {separator.join(special_lines)} |"
            text_lines.append(formatted_line)

        if other_fields:
            text_lines.append(f"其他元数据:")
            text_lines.append(f"{other_fields}")

        if text_lines:
            return "\n\n" + "\n".join(text_lines)

    return ""


mail_field_description = {
    "to": {
        "type": "list",
        "items": {
            "type": "str",
            "pattern": r"^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$",
        },
        "uniqueItems": True,
        "min": 1,
        "required": True,
    },
    "subject": {
        "type": "str",
        "required": True,
        "min": 1,
    },
    "content": {
        "type": "str",
        "required": True,
        "default": "",
    },
    "cc": {
        "type": "list",
        "required": False,
        "default": [],
        "items": {
            "type": "str",
            "pattern": r"^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$",
        },
        "uniqueItems": True
    },
    "bcc": {
        "type": "list",
        "required": False,
        "default": [],
        "items": {
            "type": "str",
            "pattern": r"^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$",
        },
        "uniqueItems": True
    },
    "html": {
        "type": "bool",
        "required": False,
        "default": False,
    },
    "attachments": {
        "type": "list",
        "required": False,
        "default": [],
        "readOnly": False,
        "items": {
            "type": "dict",
            "contains": ["content", "filename", "content_type",],
        }
    },
    "metadata": {
        "type": "dict",
        "description": "可选元数据，比如app和user，由于记录",
        "required": False,
        "default": {},
    }
}

def create_mail_task(
        smtp_host: str,
        smtp_port: int,
        smtp_username: str,
        smtp_password: str,

        email_msg: EmailMessage,

        smtp_from_name: str = "",
        smtp_from_email: str = "",
        smtp_use_ssl: bool = False,
        smtp_use_tls: bool = True,
        smtp_timeout: int = 30,
) -> asyncio.Task[bool]:
    """
    创建邮件发送任务

    Args:
        smtp_host: SMTP服务器主机
        smtp_port: SMTP服务器端口
        smtp_username: SMTP用户名
        smtp_password: SMTP密码
        email_msg: 邮件消息对象

        smtp_from_name: 发件人名称
        smtp_from_email: 发件人邮箱（为空则使用用户名）
        smtp_use_ssl: 是否使用SSL
        smtp_use_tls: 是否使用TLS
        smtp_timeout: 超时时间（秒）

    Returns:
        asyncio.Task[dict]: 邮件发送任务，返回结果字典
    """
    sender_email = email_msg.from_email or smtp_from_email or smtp_username
    from_header = f"{smtp_from_name} <{sender_email}>" if smtp_from_name else sender_email

    async def __send_email() -> bool:
        """邮件发送函数"""
        result = {
            "success": False,
            "message": "",
            "recipients": {
                "to": email_msg.to,
                "cc": email_msg.cc,
                "bcc": email_msg.bcc
            },
            "subject": email_msg.subject,
            "timestamp": datetime.now().isoformat()
        }

        try:
            await logger.trace(f"开始发送邮件: {email_msg.subject}")
            await logger.trace(f"收件人: {email_msg.to}")

            metadata_footer = format_metadata_for_email(email_msg.metadata, email_msg.html)
            if metadata_footer:
                if email_msg.html:
                    email_msg.content += metadata_footer
                else:
                    if not email_msg.content.endswith('\n'):
                        email_msg.content += '\n'
                    email_msg.content += metadata_footer

            if email_msg.attachments:
                mime_msg = MIMEMultipart()
            else:
                mime_msg = MIMEMultipart('alternative')

            mime_msg['From'] = from_header
            mime_msg['To'] = ', '.join(email_msg.to)
            mime_msg['Subject'] = email_msg.subject

            if email_msg.cc:
                mime_msg['Cc'] = ', '.join(email_msg.cc)
            if email_msg.bcc:
                mime_msg['Bcc'] = ', '.join(email_msg.bcc)

            content_type = 'html' if email_msg.html else 'plain'
            text_part = MIMEText(email_msg.content, content_type, 'utf-8')

            if email_msg.attachments:
                mime_msg.attach(text_part)
            else:
                mime_msg.attach(text_part)

            for attachment in email_msg.attachments:
                await logger.trace(f"添加附件: {attachment.filename}")
                attachment_part = MIMEBase(*attachment.content_type.split('/'))
                attachment_part.set_payload(attachment.decoded_content)
                encoders.encode_base64(attachment_part)
                attachment_part.add_header(
                    'Content-Disposition',
                    f'attachment; filename="{attachment.filename}"'
                )
                mime_msg.attach(attachment_part)

            if smtp_use_ssl:
                smtp_client = aiosmtplib.SMTP(
                    hostname=smtp_host,
                    port=smtp_port,
                    use_tls=smtp_use_tls,
                    timeout=smtp_timeout
                )
            else:
                smtp_client = aiosmtplib.SMTP(
                    hostname=smtp_host,
                    port=smtp_port,
                    timeout=smtp_timeout
                )

            await smtp_client.connect()
            await logger.trace(f"已连接到SMTP服务器: {smtp_host}:{smtp_port}")

            if not smtp_use_ssl and smtp_use_tls:
                await smtp_client.starttls()
                await logger.trace("已启用TLS加密")

            await smtp_client.login(smtp_username, smtp_password)
            await logger.trace("SMTP登录成功")

            recipients = email_msg.to + email_msg.cc + email_msg.bcc
            response = await smtp_client.send_message(mime_msg, recipients=recipients)

            await smtp_client.quit()
            await logger.trace("SMTP连接已关闭")

            result["success"] = True
            result["message"] = "邮件发送成功"
            result["response"] = str(response)

            await logger.trace(f"邮件发送成功: {email_msg.subject}")
            await logger.trace(f"收件人数: {len(recipients)}")

        except aiosmtplib.SMTPException as e:
            error_msg = f"SMTP错误: {e}"
            result["message"] = error_msg
            result["error"] = str(e)
            await logger.error(result)

        except ConnectionError as e:
            error_msg = f"连接错误: {e}"
            result["message"] = error_msg
            result["error"] = str(e)
            await logger.error(result)

        except TimeoutError as e:
            error_msg = f"超时错误: {e}"
            result["message"] = error_msg
            result["error"] = str(e)
            await logger.error(result)

        except Exception as e:
            error_msg = f"未知错误: {e}"
            result["message"] = error_msg
            result["error"] = str(e)
            await logger.error(result)

        return result["success"]

    return asyncio.create_task(__send_email())
