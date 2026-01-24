from common import ConfigLoader
from .mail import (
    create_mail_task as ct, EmailMessage, EmailAttachment, mail_field_description,
)

config = ConfigLoader()

def create_mail_task(email: EmailMessage):
    return ct(
        smtp_host=config.config.SMTP.Host,
        smtp_port=config.config.SMTP.Port,
        smtp_username=config.config.SMTP.Username,
        smtp_password=config.config.SMTP.Password,
        email_msg=email,
        smtp_from_name= config.config.SMTP.FromName,
        smtp_from_email= "",
        smtp_use_tls = config.config.SMTP.TLS,
        smtp_timeout = 30,
    )

__all__ = [
    "create_mail_task", "EmailMessage", "EmailAttachment", "mail_field_description",
]
